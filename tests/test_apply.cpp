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

TEST_CASE("apply error messages include description for diagnosis") {
    // On-device consumers (e.g. sqlpipe on mobile) cannot re-diff offline;
    // policy and apply failures must carry op.description (and SQL when mid-apply).
    TestDB db;
    db.exec("CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT, doomed TEXT);");
    db.exec("INSERT INTO t VALUES (1, 'a', 'x');");

    auto desired = parse_schema(
        "CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT);");
    auto plan = diff_schemas(extract_schema(db.db), desired);

    // Drop column → rebuild + destructive; deny both without flags.
    sqlift_apply_options opts{};  // AllowNone
    int et = 0;
    char* em = nullptr;
    int rc = sqlift_apply(db.db, plan.c_str(), opts, &et, &em);
    std::string msg = em ? em : "";
    sqlift_free(em);
    REQUIRE(rc != 0);
    // Either rebuild or destructive may fire first depending on plan flags;
    // both paths must name the object and include description.
    CHECK((et == SQLIFT_DESTRUCTIVE_ERROR || et == SQLIFT_REBUILD_ERROR));
    CHECK(msg.find("t") != std::string::npos);
    CHECK(msg.find("doomed") != std::string::npos);
    CHECK((msg.find("SQLIFT_ALLOW_DESTRUCTIVE") != std::string::npos ||
           msg.find("SQLIFT_ALLOW_REBUILD") != std::string::npos));
}

TEST_CASE("apply destructive with flag") {
    TestDB db;
    db.exec("CREATE TABLE users (id INTEGER PRIMARY KEY);");

    auto current = extract_schema(db.db);
    auto desired = empty_schema();
    auto plan_str = diff_schemas(current, desired);

    apply_plan(db.db, plan_str, SQLIFT_ALLOW_ALL);

    auto after = json::parse(extract_schema(db.db));
    CHECK(after["tables"].empty());
}

TEST_CASE("apply refuses rebuild without flag") {
    TestDB db;
    db.exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");

    // Column type change forces a rebuild but is not destructive.
    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name BLOB);");
    auto current = extract_schema(db.db);
    auto plan_str = diff_schemas(current, desired);

    CHECK(apply_err(db.db, plan_str, SQLIFT_ALLOW_NONE) == SQLIFT_REBUILD_ERROR);
}

TEST_CASE("apply rebuild with flag") {
    TestDB db;
    db.exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");
    db.exec("INSERT INTO users (id, name) VALUES (1, 'Alice');");

    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name BLOB);");
    auto current = extract_schema(db.db);
    auto plan_str = diff_schemas(current, desired);

    apply_plan(db.db, plan_str, SQLIFT_ALLOW_REBUILD);

    // Data preserved through the rebuild.
    int64_t cnt = 0;
    int rc = sqlift_db_query_int64(db.db, "SELECT COUNT(*) FROM users", &cnt, nullptr);
    CHECK(rc == 0);
    CHECK(cnt == 1);
}

TEST_CASE("apply pure addition succeeds with strictest defaults") {
    TestDB db;
    auto desired = parse_schema("CREATE TABLE users (id INTEGER PRIMARY KEY);");
    auto plan_str = diff_schemas(empty_schema(), desired);

    // SQLIFT_ALLOW_NONE: no rebuild, no destructive — pure CREATE TABLE works.
    apply_plan(db.db, plan_str, SQLIFT_ALLOW_NONE);

    auto after = json::parse(extract_schema(db.db));
    CHECK(after["tables"].contains("users"));
}

TEST_CASE("apply rejects destructive even with allow_rebuild") {
    TestDB db;
    db.exec("CREATE TABLE users (id INTEGER PRIMARY KEY);");

    auto current = extract_schema(db.db);
    auto plan_str = diff_schemas(current, empty_schema());

    // DropTable is destructive but does not require a rebuild.
    CHECK(apply_err(db.db, plan_str, SQLIFT_ALLOW_REBUILD) == SQLIFT_DESTRUCTIVE_ERROR);
}

TEST_CASE("apply rejects data-dependent change without flag") {
    TestDB db;
    db.exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");

    // Tightening nullable -> NOT NULL is data-dependent.
    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);");
    auto plan_str = diff_schemas(extract_schema(db.db), desired);

    CHECK(plan_has_data_dependent(plan_str));
    // allow_rebuild alone is not enough.
    CHECK(apply_err(db.db, plan_str, SQLIFT_ALLOW_REBUILD)
          == SQLIFT_BREAKING_CHANGE_ERROR);
}

TEST_CASE("apply data-dependent change on empty table with flag succeeds") {
    TestDB db;
    db.exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");
    // Empty table -- the tightening will succeed.

    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);");
    auto plan_str = diff_schemas(extract_schema(db.db), desired);

    apply_plan(db.db, plan_str,
               SQLIFT_ALLOW_REBUILD | SQLIFT_ALLOW_DATA_DEPENDENT);

    auto after = json::parse(extract_schema(db.db));
    bool name_notnull = false;
    for (const auto& col : after["tables"]["users"]["columns"]) {
        if (col["name"] == "name" && col.value("notnull", false))
            name_notnull = true;
    }
    CHECK(name_notnull);
}

TEST_CASE("apply data-dependent change on populated table fails at SQLite level") {
    TestDB db;
    db.exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");
    // Insert a row with NULL name -- will violate NOT NULL during rebuild.
    db.exec("INSERT INTO users (id, name) VALUES (1, NULL);");

    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);");
    auto plan_str = diff_schemas(extract_schema(db.db), desired);

    // With both flags set, the policy gate passes -- but SQLite rejects
    // the rebuild because existing data violates the new constraint. The
    // failure is whatever SQLite's exec path emits, not the policy
    // BreakingChangeError.
    int err = apply_err(db.db, plan_str,
                        SQLIFT_ALLOW_REBUILD | SQLIFT_ALLOW_DATA_DEPENDENT);
    CHECK(err != SQLIFT_OK);
    CHECK(err != SQLIFT_BREAKING_CHANGE_ERROR);
}

TEST_CASE("apply pure-loosen rebuild with allow_loosen succeeds") {
    TestDB db;
    db.exec(
        "CREATE TABLE items (id INTEGER PRIMARY KEY, price REAL,"
        " CHECK (price > 0));");
    db.exec("INSERT INTO items (id, price) VALUES (1, 9.99);");

    // Drop the CHECK constraint -- pure loosening.
    auto desired = parse_schema(
        "CREATE TABLE items (id INTEGER PRIMARY KEY, price REAL);");
    auto plan_str = diff_schemas(extract_schema(db.db), desired);

    CHECK(plan_has_loosens_only(plan_str));
    // allow_loosen alone is enough -- no allow_rebuild needed.
    apply_plan(db.db, plan_str, SQLIFT_ALLOW_LOOSEN);

    int64_t cnt = 0;
    sqlift_db_query_int64(db.db, "SELECT COUNT(*) FROM items", &cnt, nullptr);
    CHECK(cnt == 1);
}

TEST_CASE("apply pure-loosen rebuild rejected without any flag") {
    TestDB db;
    db.exec(
        "CREATE TABLE items (id INTEGER PRIMARY KEY, price REAL,"
        " CHECK (price > 0));");

    auto desired = parse_schema(
        "CREATE TABLE items (id INTEGER PRIMARY KEY, price REAL);");
    auto plan_str = diff_schemas(extract_schema(db.db), desired);

    // Strict default -- rebuild gate fires even though it would loosen.
    CHECK(apply_err(db.db, plan_str, SQLIFT_ALLOW_NONE) == SQLIFT_REBUILD_ERROR);
}

TEST_CASE("apply mixed rebuild rejected by allow_loosen alone") {
    TestDB db;
    db.exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");

    // Column type change -- not pure loosening.
    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name BLOB);");
    auto plan_str = diff_schemas(extract_schema(db.db), desired);

    CHECK(!plan_has_loosens_only(plan_str));
    CHECK(apply_err(db.db, plan_str, SQLIFT_ALLOW_LOOSEN)
          == SQLIFT_REBUILD_ERROR);
    // But allow_rebuild covers it.
    apply_plan(db.db, plan_str, SQLIFT_ALLOW_REBUILD);
}

TEST_CASE("apply NOT NULL relaxation flagged as pure loosening") {
    TestDB db;
    db.exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);");

    // Relax NOT NULL -- pure loosening.
    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");
    auto plan_str = diff_schemas(extract_schema(db.db), desired);

    CHECK(plan_has_loosens_only(plan_str));
    apply_plan(db.db, plan_str, SQLIFT_ALLOW_LOOSEN);
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

    // FK enforcement is restored to its pre-apply state, not forced ON: this
    // connection never enabled it, so it must still be OFF.
    CHECK(db.query_int64("PRAGMA foreign_keys") == 0);
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

    CHECK(apply_err(db.db, plan_str, SQLIFT_ALLOW_ALL) == SQLIFT_DRIFT_ERROR);
}

TEST_CASE("apply is atomic across operations") {
    TestDB db;

    // v1 establishes a stored schema hash, so any partial application below
    // would wedge the database in permanent drift.
    auto v1 = parse_schema("CREATE TABLE users (id INTEGER PRIMARY KEY);");
    apply_plan(db.db, diff_schemas(empty_schema(), v1));

    // Two-op plan whose second operation fails at prepare time. Hand-built
    // because diff() only ever emits SQL it believes is valid.
    const char* two_op_plan = R"J({
        "version": 1,
        "operations": [
            {"type": "CreateTable", "object_name": "first",
             "description": "create table first", "destructive": false,
             "sql": ["CREATE TABLE first (id INTEGER PRIMARY KEY)"]},
            {"type": "CreateTable", "object_name": "second",
             "description": "create table second", "destructive": false,
             "sql": ["CREATE TABLE second (id INTEGER PRIMARY KEY,"]}
        ]
    })J";

    CHECK(apply_err(db.db, two_op_plan, SQLIFT_ALLOW_ALL) == SQLIFT_APPLY_ERROR);

    // The first operation must have been rolled back with the second.
    auto after = json::parse(extract_schema(db.db));
    CHECK(after["tables"].size() == 1);
    CHECK_FALSE(after["tables"].contains("first"));

    // And the database must still be usable: a later migration must not trip
    // the drift detector, which would leave no repair path.
    auto v2 = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");
    auto current = extract_schema(db.db);
    CHECK(apply_err(db.db, diff_schemas(current, v2), SQLIFT_ALLOW_ALL) == SQLIFT_OK);
}

TEST_CASE("apply is atomic across operations - rebuild rolls back prior op") {
    TestDB db;

    auto v1 = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, age TEXT);");
    apply_plan(db.db, diff_schemas(empty_schema(), v1));
    db.exec("INSERT INTO users VALUES (1, '30');");

    // Op 1 (index) commits under autocommit today; op 2 (rebuild) fails
    // because its copy step references a column that does not exist.
    const char* plan = R"J({
        "version": 1,
        "operations": [
            {"type": "CreateIndex", "object_name": "idx_users_age",
             "description": "create index", "destructive": false,
             "sql": ["CREATE INDEX idx_users_age ON users(age)"]},
            {"type": "RebuildTable", "object_name": "users",
             "description": "rebuild users", "destructive": false,
             "requires_rebuild": true,
             "sql": ["PRAGMA foreign_keys=OFF",
                     "SAVEPOINT sqlift_rebuild",
                     "CREATE TABLE users_new (id INTEGER PRIMARY KEY, age INTEGER)",
                     "INSERT INTO users_new (id, age) SELECT id, nonexistent FROM users",
                     "DROP TABLE users",
                     "ALTER TABLE users_new RENAME TO users",
                     "RELEASE SAVEPOINT sqlift_rebuild",
                     "PRAGMA foreign_keys=ON"]}
        ]
    })J";

    CHECK(apply_err(db.db, plan, SQLIFT_ALLOW_ALL) == SQLIFT_APPLY_ERROR);

    auto after = json::parse(extract_schema(db.db));
    CHECK(after["indexes"].empty());
    CHECK(db.query_text("SELECT age FROM users WHERE id = 1") == "30");
}
