#include <doctest/doctest.h>
#include "test_helpers.h"

#include <nlohmann/json.hpp>

using json = nlohmann::json;

TEST_CASE("roundtrip: empty to schema") {
    auto sql =
        "CREATE TABLE users ("
        "  id INTEGER PRIMARY KEY,"
        "  name TEXT NOT NULL,"
        "  email TEXT"
        ");"
        "CREATE INDEX idx_email ON users(email);";

    auto desired_str = parse_schema(sql);
    auto desired = json::parse(desired_str);

    TestDB db;
    auto current = extract_schema(db.db);
    auto plan_str = diff_schemas(current, desired_str);
    apply_plan(db.db, plan_str);

    auto after = json::parse(extract_schema(db.db));
    CHECK(after["tables"].size() == desired["tables"].size());
    CHECK(after["indexes"].size() == desired["indexes"].size());

    // Verify column structure matches
    const auto& dt = desired["tables"]["users"]["columns"];
    const auto& at = after["tables"]["users"]["columns"];
    REQUIRE(dt.size() == at.size());
    for (size_t i = 0; i < dt.size(); ++i) {
        CHECK(dt[i]["name"] == at[i]["name"]);
        CHECK(dt[i]["type"] == at[i]["type"]);
        CHECK(dt[i]["notnull"] == at[i]["notnull"]);
        CHECK(dt[i]["pk"] == at[i]["pk"]);
    }
}

TEST_CASE("roundtrip: idempotent apply") {
    auto sql = "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);";
    auto desired = parse_schema(sql);

    TestDB db;
    apply_plan(db.db, diff_schemas(extract_schema(db.db), desired));

    // Second diff should be empty
    auto plan_str = diff_schemas(extract_schema(db.db), desired);
    CHECK(json::parse(plan_str)["operations"].empty());
}

TEST_CASE("roundtrip: v1 to v2 migration") {
    auto v1_sql = "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);";
    auto v2_sql =
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);"
        "CREATE TABLE posts ("
        "  id INTEGER PRIMARY KEY,"
        "  user_id INTEGER REFERENCES users(id),"
        "  title TEXT NOT NULL"
        ");";

    TestDB db;

    // Apply v1
    auto v1 = parse_schema(v1_sql);
    apply_plan(db.db, diff_schemas(extract_schema(db.db), v1));

    // Insert data
    db.exec("INSERT INTO users VALUES (1, 'Alice');");

    // Apply v2
    auto v2 = parse_schema(v2_sql);
    auto plan_str = diff_schemas(extract_schema(db.db), v2);
    apply_plan(db.db, plan_str);

    // Verify data preserved
    CHECK(db.query_text("SELECT name FROM users WHERE id = 1") == "Alice");

    // Verify new table exists
    auto after = json::parse(extract_schema(db.db));
    CHECK(after["tables"].contains("posts"));
    CHECK(after["tables"]["users"]["columns"].size() == 3);

    // Idempotent check
    auto plan2_str = diff_schemas(extract_schema(db.db), v2);
    CHECK(json::parse(plan2_str)["operations"].empty());
}

TEST_CASE("cross-language hash: known DDL produces expected SHA-256") {
    // This DDL and expected hash are shared with the Go test suite
    // (go/sqlift/schema_test.go TestCrossLanguageHash). If either the C++
    // or Go hash implementation diverges, one test will fail.
    auto ddl =
        "CREATE TABLE users ("
        "    id INTEGER PRIMARY KEY,"
        "    name TEXT NOT NULL,"
        "    email TEXT COLLATE NOCASE,"
        "    age INTEGER CHECK(age > 0),"
        "    FOREIGN KEY (id) REFERENCES users(id) ON DELETE CASCADE ON UPDATE NO ACTION"
        ");"
        "CREATE TABLE posts ("
        "    id INTEGER PRIMARY KEY,"
        "    user_id INTEGER NOT NULL REFERENCES users(id),"
        "    title TEXT NOT NULL DEFAULT '',"
        "    body TEXT"
        ");"
        "CREATE INDEX idx_posts_user ON posts(user_id);"
        "CREATE UNIQUE INDEX idx_users_email ON users(email);"
        "CREATE VIEW active_users AS SELECT id, name FROM users WHERE age > 18;"
        "CREATE TRIGGER trg_posts_delete AFTER DELETE ON posts BEGIN SELECT 1; END;";

    auto schema_str = parse_schema(ddl);
    CHECK(schema_hash(schema_str) == "e712ade60030bfb83109e2bc49ba2d6d3025ade275dffde2a33ea5279dc99c13");
}

TEST_CASE("roundtrip: v1 to v2 to v3 breaking change rejected") {
    auto v1 = "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);";
    auto v2 = "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);";
    auto v3 = "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL);";

    TestDB db;

    // v1
    apply_plan(db.db, diff_schemas(extract_schema(db.db), parse_schema(v1)));
    db.exec("INSERT INTO users VALUES (1, 'Alice');");

    // v2
    apply_plan(db.db, diff_schemas(extract_schema(db.db), parse_schema(v2)));

    // v3 makes email NOT NULL — this is a breaking change and must be rejected
    CHECK(diff_err(extract_schema(db.db), parse_schema(v3)) == SQLIFT_BREAKING_CHANGE_ERROR);
}
