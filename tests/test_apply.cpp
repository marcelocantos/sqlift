#include <doctest/doctest.h>
#include "test_helpers.h"

#include <nlohmann/json.hpp>
using json = nlohmann::json;

TEST_CASE("apply create table") {
    TestDB db;
    auto desired = parse_schema("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");
    auto current = extract_schema(db.db);
    auto plan_str = diff_schemas(current, desired);

    apply_plan(db.db, plan_str);

    auto after = json::parse(extract_schema(db.db));
    // Compare structurally (ignore raw_sql differences)
    REQUIRE(after["tables"].size() == 1);
    CHECK(after["tables"].contains("users"));
    CHECK(after["tables"]["users"]["columns"].size() == 2);
}

TEST_CASE("apply add column") {
    TestDB db;
    db.exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");
    db.exec("INSERT INTO users VALUES (1, 'Alice');");

    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);");
    auto current = extract_schema(db.db);
    auto plan_str = diff_schemas(current, desired);

    apply_plan(db.db, plan_str);

    // Verify data preserved
    CHECK(db.query_text("SELECT name FROM users WHERE id = 1") == "Alice");

    // Verify new column exists
    auto after = json::parse(extract_schema(db.db));
    CHECK(after["tables"]["users"]["columns"].size() == 3);
}

TEST_CASE("apply rebuild table - change column type") {
    TestDB db;
    db.exec("CREATE TABLE users (id INTEGER PRIMARY KEY, age TEXT);");
    db.exec("INSERT INTO users VALUES (1, '30');");

    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, age INTEGER);");
    auto current = extract_schema(db.db);
    auto plan_str = diff_schemas(current, desired);

    apply_plan(db.db, plan_str);

    // Data should be preserved (SQLite will coerce TEXT '30' to INTEGER 30)
    CHECK(db.query_text("SELECT age FROM users WHERE id = 1") == "30");
}

TEST_CASE("apply refuses destructive without flag") {
    TestDB db;
    db.exec("CREATE TABLE users (id INTEGER PRIMARY KEY);");

    auto current = extract_schema(db.db);
    auto desired = empty_schema();
    auto plan_str = diff_schemas(current, desired);

    CHECK(apply_err(db.db, plan_str) == SQLIFT_DESTRUCTIVE_ERROR);
}

TEST_CASE("apply destructive with flag") {
    TestDB db;
    db.exec("CREATE TABLE users (id INTEGER PRIMARY KEY);");

    auto current = extract_schema(db.db);
    auto desired = empty_schema();
    auto plan_str = diff_schemas(current, desired);

    apply_plan(db.db, plan_str, true);

    auto after = json::parse(extract_schema(db.db));
    CHECK(after["tables"].empty());
}

TEST_CASE("apply updates state hash") {
    TestDB db;
    auto desired = parse_schema("CREATE TABLE users (id INTEGER PRIMARY KEY);");
    auto plan_str = diff_schemas(empty_schema(), desired);

    apply_plan(db.db, plan_str);

    // Verify _sqlift_state exists and has a hash
    CHECK(!db.query_text("SELECT value FROM _sqlift_state WHERE key = 'schema_hash'").empty());
}

TEST_CASE("apply FK violation includes parent table and rowid") {
    TestDB db;
    db.exec("PRAGMA foreign_keys=OFF;");
    db.exec(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id));"
        "INSERT INTO users VALUES (1, 'Alice');"
        "INSERT INTO orders VALUES (1, 1);"
        "INSERT INTO orders VALUES (2, 999);");  // orphan FK
    db.exec("PRAGMA foreign_keys=ON;");

    // Change column type on orders to trigger a rebuild — FK is unchanged so
    // no BreakingChangeError, but the orphan data causes an FK violation.
    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id BIGINT REFERENCES users(id));");
    auto current = extract_schema(db.db);
    auto plan_str = diff_schemas(current, desired);

    std::string msg;
    int err = apply_err_msg(db.db, plan_str, msg);
    CHECK(err == SQLIFT_APPLY_ERROR);
    CHECK(msg.find("orders") != std::string::npos);
    CHECK(msg.find("users") != std::string::npos);
    CHECK(msg.find("rowid") != std::string::npos);
}

TEST_CASE("apply error recovery preserves database state") {
    TestDB db;
    db.exec("PRAGMA foreign_keys=OFF;");
    db.exec(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "INSERT INTO users VALUES (1, 'Alice');"
        "CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id));"
        "INSERT INTO orders VALUES (1, 1);"
        "INSERT INTO orders VALUES (2, 999);");  // orphan FK
    db.exec("PRAGMA foreign_keys=ON;");

    auto current = extract_schema(db.db);
    // Change column type on orders to trigger a rebuild — FK is unchanged
    // so no BreakingChangeError, but the orphan data causes an FK violation.
    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id BIGINT REFERENCES users(id));");

    auto plan_str = diff_schemas(current, desired);

    // This should fail during the FK check step of rebuild
    CHECK(apply_err(db.db, plan_str) == SQLIFT_APPLY_ERROR);

    // Verify the original orders table still has its data
    CHECK(db.query_int64("SELECT count(*) FROM orders") == 2);

    // Verify no temp table left behind
    CHECK(db.query_int64("SELECT count(*) FROM sqlite_master WHERE name LIKE '%sqlift_new%'") == 0);

    // Verify FK enforcement is restored to ON after failed apply (T1)
    CHECK(db.query_int64("PRAGMA foreign_keys") == 1);
}

TEST_CASE("apply restores FK enforcement ON after successful rebuild") {
    TestDB db;
    db.exec("PRAGMA foreign_keys=ON;");
    db.exec(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "INSERT INTO users VALUES (1, 'Alice');");

    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name INTEGER);");
    auto current = extract_schema(db.db);
    auto plan_str = diff_schemas(current, desired);

    apply_plan(db.db, plan_str);

    CHECK(db.query_int64("PRAGMA foreign_keys") == 1);
}

TEST_CASE("schema hash is deterministic") {
    auto s1 = parse_schema("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");
    auto s2 = parse_schema("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");
    CHECK(schema_hash(s1) == schema_hash(s2));
}

TEST_CASE("schema hash differs for different schemas") {
    auto s1 = parse_schema("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");
    auto s2 = parse_schema("CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);");
    CHECK(schema_hash(s1) != schema_hash(s2));
}

TEST_CASE("apply rebuilds multiple tables") {
    TestDB db;
    db.exec(
        "CREATE TABLE a (id INTEGER PRIMARY KEY, x TEXT);"
        "CREATE TABLE b (id INTEGER PRIMARY KEY, y TEXT);"
        "INSERT INTO a VALUES (1, 'aa');"
        "INSERT INTO b VALUES (1, 'bb');");

    auto desired = parse_schema(
        "CREATE TABLE a (id INTEGER PRIMARY KEY, x INTEGER);"
        "CREATE TABLE b (id INTEGER PRIMARY KEY, y INTEGER);");
    auto current = extract_schema(db.db);
    auto plan_str = diff_schemas(current, desired);

    apply_plan(db.db, plan_str);

    // Verify both tables rebuilt with data preserved
    CHECK(db.query_text("SELECT x FROM a WHERE id = 1") == "aa");
    CHECK(db.query_text("SELECT y FROM b WHERE id = 1") == "bb");

    // Verify FK enforcement still ON after rebuild
    CHECK(db.query_int64("PRAGMA foreign_keys") == 1);
}

TEST_CASE("migration_version starts at 0") {
    TestDB db;
    CHECK(migration_ver(db.db) == 0);
}

TEST_CASE("migration_version increments on apply") {
    TestDB db;

    // First migration
    auto v1 = parse_schema("CREATE TABLE users (id INTEGER PRIMARY KEY);");
    apply_plan(db.db, diff_schemas(empty_schema(), v1));
    CHECK(migration_ver(db.db) == 1);

    // Second migration
    auto v2 = parse_schema("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");
    auto current = extract_schema(db.db);
    apply_plan(db.db, diff_schemas(current, v2));
    CHECK(migration_ver(db.db) == 2);
}

TEST_CASE("migration_version survives no-op apply") {
    TestDB db;

    auto v1 = parse_schema("CREATE TABLE users (id INTEGER PRIMARY KEY);");
    apply_plan(db.db, diff_schemas(empty_schema(), v1));
    CHECK(migration_ver(db.db) == 1);

    // No-op (same schema) — should not increment
    auto current = extract_schema(db.db);
    auto plan_str = diff_schemas(current, v1);
    CHECK(json::parse(plan_str)["operations"].empty());
    // Version unchanged since apply() returns early for empty plans
    CHECK(migration_ver(db.db) == 1);
}

TEST_CASE("apply detects drift") {
    TestDB db;

    // First migration
    auto v1 = parse_schema("CREATE TABLE users (id INTEGER PRIMARY KEY);");
    apply_plan(db.db, diff_schemas(empty_schema(), v1));

    // Modify schema outside sqlift
    db.exec("ALTER TABLE users ADD COLUMN sneaky TEXT;");

    // Try to apply another migration
    auto v2 = parse_schema("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");
    auto current = extract_schema(db.db);
    auto plan_str = diff_schemas(current, v2);

    CHECK(apply_err(db.db, plan_str, true) == SQLIFT_DRIFT_ERROR);
}
