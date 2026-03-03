// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

#include <doctest/doctest.h>
#include "test_helpers.h"

#include <nlohmann/json.hpp>
using json = nlohmann::json;

// ---------------------------------------------------------------------------
// OpType string tests — exercised via plan JSON from diff
// ---------------------------------------------------------------------------

TEST_CASE("op type strings appear correctly in plan JSON") {
    // CreateTable / DropTable / AddColumn
    auto s_none  = empty_schema();
    auto s_users = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");
    auto s_users2 = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);");

    auto plan_create = json::parse(diff_schemas(s_none, s_users));
    REQUIRE(!plan_create["operations"].empty());
    CHECK(plan_create["operations"][0]["type"] == "CreateTable");

    auto plan_drop = json::parse(diff_schemas(s_users, s_none));
    REQUIRE(!plan_drop["operations"].empty());
    CHECK(plan_drop["operations"][0]["type"] == "DropTable");

    auto plan_add = json::parse(diff_schemas(s_users, s_users2));
    REQUIRE(!plan_add["operations"].empty());
    CHECK(plan_add["operations"][0]["type"] == "AddColumn");

    // RebuildTable
    auto s_rebuild_src = parse_schema(
        "CREATE TABLE t (id INTEGER PRIMARY KEY, age TEXT);");
    auto s_rebuild_dst = parse_schema(
        "CREATE TABLE t (id INTEGER PRIMARY KEY, age INTEGER);");
    auto plan_rebuild = json::parse(diff_schemas(s_rebuild_src, s_rebuild_dst));
    REQUIRE(!plan_rebuild["operations"].empty());
    CHECK(plan_rebuild["operations"][0]["type"] == "RebuildTable");

    // CreateIndex / DropIndex
    auto s_idx = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE INDEX idx_name ON users(name);");
    auto plan_create_idx = json::parse(diff_schemas(s_users, s_idx));
    bool found_create_index = false;
    for (auto& op : plan_create_idx["operations"])
        if (op["type"] == "CreateIndex") { found_create_index = true; break; }
    CHECK(found_create_index);

    auto plan_drop_idx = json::parse(diff_schemas(s_idx, s_users));
    bool found_drop_index = false;
    for (auto& op : plan_drop_idx["operations"])
        if (op["type"] == "DropIndex") { found_drop_index = true; break; }
    CHECK(found_drop_index);

    // CreateView / DropView
    auto s_view = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE VIEW vw_users AS SELECT * FROM users;");
    auto plan_create_view = json::parse(diff_schemas(s_users, s_view));
    bool found_create_view = false;
    for (auto& op : plan_create_view["operations"])
        if (op["type"] == "CreateView") { found_create_view = true; break; }
    CHECK(found_create_view);

    auto plan_drop_view = json::parse(diff_schemas(s_view, s_users));
    bool found_drop_view = false;
    for (auto& op : plan_drop_view["operations"])
        if (op["type"] == "DropView") { found_drop_view = true; break; }
    CHECK(found_drop_view);

    // CreateTrigger / DropTrigger
    auto s_trig = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE TRIGGER trg AFTER DELETE ON users BEGIN SELECT 1; END;");
    auto plan_create_trig = json::parse(diff_schemas(s_users, s_trig));
    bool found_create_trig = false;
    for (auto& op : plan_create_trig["operations"])
        if (op["type"] == "CreateTrigger") { found_create_trig = true; break; }
    CHECK(found_create_trig);

    auto plan_drop_trig = json::parse(diff_schemas(s_trig, s_users));
    bool found_drop_trig = false;
    for (auto& op : plan_drop_trig["operations"])
        if (op["type"] == "DropTrigger") { found_drop_trig = true; break; }
    CHECK(found_drop_trig);
}

// ---------------------------------------------------------------------------
// Plan JSON structure
// ---------------------------------------------------------------------------

TEST_CASE("json round-trip: empty plan") {
    auto schema = parse_schema("CREATE TABLE t (id INTEGER PRIMARY KEY);");
    auto plan_str = diff_schemas(schema, schema);
    auto plan = json::parse(plan_str);
    CHECK(plan["operations"].is_array());
    CHECK(plan["operations"].empty());
}

TEST_CASE("json round-trip: plan with multiple operation types") {
    auto current = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE TABLE old_logs (id INTEGER PRIMARY KEY);"
        "CREATE INDEX idx_name ON users(name);"
        "CREATE VIEW all_users AS SELECT * FROM users;");

    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);"
        "CREATE TABLE posts (id INTEGER PRIMARY KEY, title TEXT);"
        "CREATE VIEW active_users AS SELECT * FROM users;");

    auto plan_str = diff_schemas(current, desired);
    auto plan = json::parse(plan_str);

    REQUIRE(!plan["operations"].empty());

    // Every operation must have the required fields with expected types.
    for (auto& op : plan["operations"]) {
        CHECK(op["type"].is_string());
        CHECK(op["object_name"].is_string());
        CHECK(op["description"].is_string());
        CHECK(op["sql"].is_array());
        CHECK(op["destructive"].is_boolean());
    }

    // Set up a DB with the current schema, then apply the diff plan.
    TestDB db;
    apply_plan(db.db, diff_schemas(empty_schema(), current));
    apply_plan(db.db, plan_str, /*allow_destructive=*/true);
    auto after = json::parse(extract_schema(db.db));
    CHECK(after["tables"].contains("users"));
    CHECK(after["tables"].contains("posts"));
    CHECK(!after["tables"].contains("old_logs"));
    CHECK(after["views"].contains("active_users"));
    CHECK(!after["views"].contains("all_users"));
}

TEST_CASE("json round-trip: destructive operations") {
    auto current = parse_schema("CREATE TABLE users (id INTEGER PRIMARY KEY);");
    auto plan_str = diff_schemas(current, empty_schema());
    auto plan = json::parse(plan_str);

    REQUIRE(!plan["operations"].empty());
    bool has_destructive = false;
    bool has_drop_table  = false;
    for (auto& op : plan["operations"]) {
        if (op["destructive"].get<bool>()) has_destructive = true;
        if (op["type"] == "DropTable")     has_drop_table  = true;
    }
    CHECK(has_destructive);
    CHECK(has_drop_table);

    // Can be applied with allow_destructive=true.
    TestDB db;
    db.exec("CREATE TABLE users (id INTEGER PRIMARY KEY);");
    apply_plan(db.db, plan_str, /*allow_destructive=*/true);
    auto after = json::parse(extract_schema(db.db));
    CHECK(after["tables"].empty());
}

TEST_CASE("json round-trip: rebuild table preserves multi-statement sql") {
    auto current = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, age TEXT);");
    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, age INTEGER);");

    auto plan_str = diff_schemas(current, desired);
    auto plan = json::parse(plan_str);

    REQUIRE(plan["operations"].size() == 1);
    CHECK(plan["operations"][0]["type"] == "RebuildTable");
    CHECK(plan["operations"][0]["sql"].size() > 1);
}

TEST_CASE("deserialized plan can be applied to a database") {
    auto desired = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);"
        "CREATE INDEX idx_name ON users(name);");

    auto plan_str = diff_schemas(empty_schema(), desired);

    // Apply the JSON plan string directly to a fresh database.
    TestDB db;
    apply_plan(db.db, plan_str);

    auto after = json::parse(extract_schema(db.db));
    CHECK(after["tables"].contains("users"));
    CHECK(after["tables"]["users"]["columns"].size() == 2);
    CHECK(after["indexes"].contains("idx_name"));
}

// ---------------------------------------------------------------------------
// apply_err: JSON validation
// ---------------------------------------------------------------------------

TEST_CASE("from_json rejects invalid JSON") {
    TestDB db;
    CHECK(apply_err(db.db, "not json") == SQLIFT_JSON_ERROR);
    CHECK(apply_err(db.db, "")         == SQLIFT_JSON_ERROR);
}

TEST_CASE("from_json rejects non-object top level") {
    TestDB db;
    CHECK(apply_err(db.db, "[1,2,3]") == SQLIFT_JSON_ERROR);
    CHECK(apply_err(db.db, "42")      == SQLIFT_JSON_ERROR);
}

TEST_CASE("from_json rejects missing version") {
    TestDB db;
    CHECK(apply_err(db.db, R"({"operations":[]})") == SQLIFT_JSON_ERROR);
}

TEST_CASE("from_json rejects unsupported version") {
    TestDB db;
    CHECK(apply_err(db.db, R"({"version":999,"operations":[]})") == SQLIFT_JSON_ERROR);
}

TEST_CASE("from_json rejects missing operations") {
    TestDB db;
    CHECK(apply_err(db.db, R"({"version":1})") == SQLIFT_JSON_ERROR);
}

TEST_CASE("from_json rejects operation with missing fields") {
    TestDB db;

    // Missing "type"
    CHECK(apply_err(db.db, R"({"version":1,"operations":[
        {"object_name":"t","description":"d","sql":["s"],"destructive":false}
    ]})") == SQLIFT_JSON_ERROR);

    // Missing "sql"
    CHECK(apply_err(db.db, R"({"version":1,"operations":[
        {"type":"CreateTable","object_name":"t","description":"d","destructive":false}
    ]})") == SQLIFT_JSON_ERROR);
}

TEST_CASE("from_json rejects unknown OpType string") {
    TestDB db;
    CHECK(apply_err(db.db, R"({"version":1,"operations":[
        {"type":"Bogus","object_name":"t","description":"d","sql":[],"destructive":false}
    ]})") == SQLIFT_JSON_ERROR);
}

// ---------------------------------------------------------------------------
// Warnings
// ---------------------------------------------------------------------------

TEST_CASE("json round-trip: warnings preserved") {
    // idx_id on users(id) is redundant — covered by the PRIMARY KEY.
    auto s = parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE INDEX idx_id ON users(id);");
    auto plan_str = diff_schemas(empty_schema(), s);
    auto plan = json::parse(plan_str);

    REQUIRE(plan.contains("warnings"));
    REQUIRE(!plan["warnings"].empty());

    bool found = false;
    for (auto& w : plan["warnings"]) {
        if (w.value("index_name", "") == "idx_id") {
            CHECK(w.value("table_name", "") == "users");
            CHECK(!w.value("covered_by", "").empty());
            found = true;
        }
    }
    CHECK(found);
}

TEST_CASE("from_json: missing warnings field is ok") {
    TestDB db;
    // An empty plan with no warnings field must apply cleanly.
    apply_plan(db.db, R"({"version":1,"operations":[]})");
}

// ---------------------------------------------------------------------------
// Schema JSON round-trips (parse → diff with itself → empty plan)
// ---------------------------------------------------------------------------

TEST_CASE("schema json round-trip: complex schema") {
    auto schema_json = parse_schema(
        "CREATE TABLE users ("
        "  id INTEGER PRIMARY KEY,"
        "  name TEXT NOT NULL,"
        "  email TEXT COLLATE NOCASE,"
        "  age INTEGER CHECK(age > 0),"
        "  CONSTRAINT fk_self FOREIGN KEY (id) REFERENCES users(id) ON DELETE CASCADE"
        ");"
        "CREATE TABLE posts ("
        "  id INTEGER PRIMARY KEY,"
        "  user_id INTEGER NOT NULL REFERENCES users(id),"
        "  title TEXT NOT NULL DEFAULT '',"
        "  body TEXT"
        ");"
        "CREATE INDEX idx_posts_user ON posts(user_id);"
        "CREATE UNIQUE INDEX idx_users_email ON users(email);"
        "CREATE VIEW active_users AS SELECT id, name FROM users WHERE age > 18;"
        "CREATE TRIGGER trg_posts_delete AFTER DELETE ON posts BEGIN SELECT 1; END;");

    // Diff schema with itself — must be empty.
    auto plan_str = diff_schemas(schema_json, schema_json);
    auto plan = json::parse(plan_str);
    CHECK(plan["operations"].empty());

    // Verify the JSON has expected top-level keys.
    auto s = json::parse(schema_json);
    CHECK(s.contains("tables"));
    CHECK(s.contains("indexes"));
    CHECK(s.contains("views"));
    CHECK(s.contains("triggers"));
    CHECK(s["tables"].contains("users"));
    CHECK(s["tables"].contains("posts"));
    CHECK(s["indexes"].contains("idx_posts_user"));
    CHECK(s["indexes"].contains("idx_users_email"));
    CHECK(s["views"].contains("active_users"));
    CHECK(s["triggers"].contains("trg_posts_delete"));

    // Hash must be stable across two identical parses.
    auto schema_json2 = parse_schema(
        "CREATE TABLE users ("
        "  id INTEGER PRIMARY KEY,"
        "  name TEXT NOT NULL,"
        "  email TEXT COLLATE NOCASE,"
        "  age INTEGER CHECK(age > 0),"
        "  CONSTRAINT fk_self FOREIGN KEY (id) REFERENCES users(id) ON DELETE CASCADE"
        ");"
        "CREATE TABLE posts ("
        "  id INTEGER PRIMARY KEY,"
        "  user_id INTEGER NOT NULL REFERENCES users(id),"
        "  title TEXT NOT NULL DEFAULT '',"
        "  body TEXT"
        ");"
        "CREATE INDEX idx_posts_user ON posts(user_id);"
        "CREATE UNIQUE INDEX idx_users_email ON users(email);"
        "CREATE VIEW active_users AS SELECT id, name FROM users WHERE age > 18;"
        "CREATE TRIGGER trg_posts_delete AFTER DELETE ON posts BEGIN SELECT 1; END;");
    CHECK(schema_hash(schema_json) == schema_hash(schema_json2));
}

TEST_CASE("schema json round-trip: empty schema") {
    auto plan_str = diff_schemas(empty_schema(), empty_schema());
    auto plan = json::parse(plan_str);
    CHECK(plan["operations"].empty());
}

TEST_CASE("schema json round-trip: WITHOUT ROWID and STRICT") {
    auto schema_json = parse_schema(
        "CREATE TABLE kv (k TEXT PRIMARY KEY, v TEXT) WITHOUT ROWID;"
        "CREATE TABLE strict_t (id INTEGER PRIMARY KEY, x TEXT) STRICT;");

    auto s = json::parse(schema_json);
    CHECK(s["tables"]["kv"].value("without_rowid", false));
    CHECK(s["tables"]["strict_t"].value("strict", false));

    // Diff with itself — must be empty.
    auto plan_str = diff_schemas(schema_json, schema_json);
    auto plan = json::parse(plan_str);
    CHECK(plan["operations"].empty());
}

TEST_CASE("schema json round-trip: generated columns") {
    auto schema_json = parse_schema(
        "CREATE TABLE t ("
        "  first TEXT,"
        "  last TEXT,"
        "  full_name TEXT GENERATED ALWAYS AS (first || ' ' || last) STORED"
        ");");

    auto s = json::parse(schema_json);
    auto& cols = s["tables"]["t"]["columns"];
    REQUIRE(cols.size() == 3);

    // Find the generated column.
    bool found = false;
    for (auto& col : cols) {
        if (col.value("name", "") == "full_name") {
            // generated == 3 corresponds to GeneratedType::Stored in the C++ enum.
            CHECK(col["generated"] == 3);
            CHECK(col.value("generated_expr", "") == "first || ' ' || last");
            found = true;
        }
    }
    CHECK(found);

    // Diff with itself — must be empty.
    auto plan_str = diff_schemas(schema_json, schema_json);
    auto plan = json::parse(plan_str);
    CHECK(plan["operations"].empty());
}

TEST_CASE("schema_from_json rejects invalid JSON") {
    // diff_err exercises the schema deserialization path on the current side.
    CHECK(diff_err("not json",  empty_schema()) == SQLIFT_JSON_ERROR);
    CHECK(diff_err("[1,2,3]",   empty_schema()) == SQLIFT_JSON_ERROR);
}

// ---------------------------------------------------------------------------
// Tampered plan detection
// ---------------------------------------------------------------------------

TEST_CASE("from_json rejects tampered plan with mismatched type and sql") {
    TestDB db;

    // CreateTable op whose sql starts with DROP TABLE — should be rejected.
    auto tampered_create =
        R"({"version":1,"operations":[)"
        R"({"type":"CreateTable","object_name":"t","description":"d",)"
        R"("sql":["DROP TABLE t"],"destructive":false}]})";
    CHECK(apply_err(db.db, tampered_create) == SQLIFT_JSON_ERROR);

    // DropTable op whose sql starts with CREATE TABLE — should be rejected.
    // Use a named raw-string delimiter because the SQL contains )".
    auto tampered_drop =
        R"x({"version":1,"operations":[)x"
        R"x({"type":"DropTable","object_name":"t","description":"d",)x"
        R"x("sql":["CREATE TABLE t (id INTEGER)"],"destructive":true}]})x";
    CHECK(apply_err(db.db, tampered_drop) == SQLIFT_JSON_ERROR);

    // RebuildTable op that does not start with the expected PRAGMA sequence.
    auto tampered_rebuild =
        R"({"version":1,"operations":[)"
        R"({"type":"RebuildTable","object_name":"t","description":"d",)"
        R"("sql":["DROP TABLE t"],"destructive":false}]})";
    CHECK(apply_err(db.db, tampered_rebuild) == SQLIFT_JSON_ERROR);

    // AddColumn op whose sql starts with DROP TABLE — should be rejected.
    auto tampered_add =
        R"({"version":1,"operations":[)"
        R"({"type":"AddColumn","object_name":"t","description":"d",)"
        R"("sql":["DROP TABLE t"],"destructive":false}]})";
    CHECK(apply_err(db.db, tampered_add) == SQLIFT_JSON_ERROR);
}
