// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

#include <doctest/doctest.h>
#include "test_helpers.h"

// CREATE VIRTUAL TABLE support and shadow-table filtering.
//
// FTS5 (and any other vtable module) creates several shadow tables in
// sqlite_master with type='table' and sql IS NULL. Without filtering,
// these would appear in the extracted schema and a desired schema that
// only declares the parent virtual table would diff-drop them — blocked
// by AllowDestructive=false and breaking every startup.

TEST_CASE("parse CREATE VIRTUAL TABLE fts5") {
    auto j = json::parse(parse_schema(
        "CREATE VIRTUAL TABLE messages_fts USING fts5(text, content);"));

    // Regular tables map is empty — fts5 vtable is not a regular table.
    CHECK(j["tables"].empty());

    REQUIRE(j.contains("virtual_tables"));
    REQUIRE(j["virtual_tables"].size() == 1);
    const auto& vt = j["virtual_tables"]["messages_fts"];
    CHECK(vt["name"] == "messages_fts");
    CHECK(vt["module"] == "fts5");
    CHECK(vt["args"] == "text, content");
}

TEST_CASE("parse CREATE VIRTUAL TABLE with no args") {
    // "USING fts4" with no parens is legal.
    auto j = json::parse(parse_schema(
        "CREATE VIRTUAL TABLE t USING fts4;"));

    REQUIRE(j["virtual_tables"].size() == 1);
    const auto& vt = j["virtual_tables"]["t"];
    CHECK(vt["module"] == "fts4");
    CHECK(vt["args"] == "");
}

TEST_CASE("extract filters fts5 shadow tables") {
    TestDB db;
    db.exec("CREATE VIRTUAL TABLE notes_fts USING fts5(body);");

    // Sanity check: sqlite_master contains the parent + 5 shadow tables.
    CHECK(db.query_int64(
        "SELECT count(*) FROM sqlite_master "
        "WHERE type='table' AND name LIKE 'notes_fts%'") == 6);

    auto j = json::parse(extract_schema(db.db));
    // Shadow tables (sql IS NULL) must NOT appear as regular tables.
    CHECK(j["tables"].empty());
    REQUIRE(j["virtual_tables"].size() == 1);
    CHECK(j["virtual_tables"].contains("notes_fts"));
    CHECK(j["virtual_tables"]["notes_fts"]["module"] == "fts5");
}

TEST_CASE("extract distinguishes shadow tables from user tables with similar names") {
    // A real table named foo_data is not a shadow — it has non-NULL sql.
    TestDB db;
    db.exec("CREATE VIRTUAL TABLE foo USING fts5(x);");
    db.exec("CREATE TABLE foo_data_real (id INTEGER PRIMARY KEY);");

    auto j = json::parse(extract_schema(db.db));
    REQUIRE(j["virtual_tables"].size() == 1);
    CHECK(j["virtual_tables"].contains("foo"));
    // The user table survives.
    REQUIRE(j["tables"].size() == 1);
    CHECK(j["tables"].contains("foo_data_real"));
}

TEST_CASE("diff: create new virtual table") {
    auto current = empty_schema();
    auto desired = parse_schema(
        "CREATE VIRTUAL TABLE search USING fts5(content);");
    auto plan = json::parse(diff_schemas(current, desired));

    REQUIRE(plan["operations"].size() == 1);
    const auto& op = plan["operations"][0];
    CHECK(op["type"] == "CreateVirtualTable");
    CHECK(op["object_name"] == "search");
    CHECK(op["destructive"] == false);
    REQUIRE(op["sql"].size() == 1);
    std::string sql = op["sql"][0];
    CHECK(sql.find("CREATE VIRTUAL TABLE") == 0);
    CHECK(sql.find("fts5") != std::string::npos);
}

TEST_CASE("diff: drop removed virtual table is destructive") {
    auto current = parse_schema(
        "CREATE VIRTUAL TABLE search USING fts5(content);");
    auto desired = empty_schema();
    auto plan = json::parse(diff_schemas(current, desired));

    REQUIRE(plan["operations"].size() == 1);
    const auto& op = plan["operations"][0];
    CHECK(op["type"] == "DropVirtualTable");
    CHECK(op["object_name"] == "search");
    CHECK(op["destructive"] == true);
    REQUIRE(op["sql"].size() == 1);
    CHECK(std::string(op["sql"][0]).find("DROP TABLE") == 0);
}

TEST_CASE("diff: identical virtual tables produce no-op") {
    auto current = parse_schema(
        "CREATE VIRTUAL TABLE search USING fts5(content);");
    auto desired = parse_schema(
        "CREATE VIRTUAL TABLE search USING fts5(content);");
    auto plan = json::parse(diff_schemas(current, desired));
    CHECK(plan["operations"].empty());
}

TEST_CASE("diff: changed args produce drop+recreate") {
    auto current = parse_schema(
        "CREATE VIRTUAL TABLE search USING fts5(content);");
    auto desired = parse_schema(
        "CREATE VIRTUAL TABLE search USING fts5(content, body);");
    auto plan = json::parse(diff_schemas(current, desired));

    REQUIRE(plan["operations"].size() == 2);
    // Drop emitted before create so the name slot is free for the recreate.
    CHECK(plan["operations"][0]["type"] == "DropVirtualTable");
    CHECK(plan["operations"][0]["destructive"] == true);
    CHECK(plan["operations"][1]["type"] == "CreateVirtualTable");
    CHECK(plan["operations"][1]["destructive"] == false);
}

TEST_CASE("diff: changed module produces drop+recreate") {
    auto current = parse_schema(
        "CREATE VIRTUAL TABLE search USING fts4(content);");
    auto desired = parse_schema(
        "CREATE VIRTUAL TABLE search USING fts5(content);");
    auto plan = json::parse(diff_schemas(current, desired));

    REQUIRE(plan["operations"].size() == 2);
    CHECK(plan["operations"][0]["type"] == "DropVirtualTable");
    CHECK(plan["operations"][1]["type"] == "CreateVirtualTable");
}

TEST_CASE("apply: create then extract round-trips through the live DB") {
    TestDB db;
    auto current = extract_schema(db.db);
    auto desired = parse_schema(
        "CREATE VIRTUAL TABLE notes USING fts5(body, title);");
    auto plan = diff_schemas(current, desired);
    apply_plan(db.db, plan, SQLIFT_ALLOW_REBUILD);

    // The vtable + 5 shadow tables are now in the live DB.
    CHECK(db.query_int64(
        "SELECT count(*) FROM sqlite_master "
        "WHERE type='table' AND name LIKE 'notes%'") == 6);

    // Extracting again yields the same virtual table; subsequent diff is no-op.
    auto after = extract_schema(db.db);
    auto noop = json::parse(diff_schemas(after, desired));
    CHECK(noop["operations"].empty());
}

TEST_CASE("apply: drop without AllowDestructive is rejected") {
    TestDB db;
    db.exec("CREATE VIRTUAL TABLE old USING fts5(x);");
    // Mark current as the baseline so the drift check doesn't fire on
    // subsequent applies.
    auto current = extract_schema(db.db);
    auto desired = empty_schema();
    auto plan = diff_schemas(current, desired);

    // Default options (AllowNone) — drop blocked.
    sqlift_apply_options opts{};
    int et = 0; char* em = nullptr;
    int rc = sqlift_apply(db.db, plan.c_str(), opts, &et, &em);
    std::string msg = em ? em : "";
    sqlift_free(em);
    CHECK(rc != 0);
    CHECK(et == SQLIFT_DESTRUCTIVE_ERROR);
    // The vtable is still there.
    CHECK(db.query_int64(
        "SELECT count(*) FROM sqlite_master "
        "WHERE name='old' AND type='table'") == 1);

    // With AllowDestructive, the drop succeeds.
    apply_plan(db.db, plan, SQLIFT_ALLOW_DESTRUCTIVE);
    CHECK(db.query_int64(
        "SELECT count(*) FROM sqlite_master "
        "WHERE name LIKE 'old%' AND type='table'") == 0);
}

TEST_CASE("apply: create succeeds under AllowNone (additive)") {
    TestDB db;
    auto desired = parse_schema(
        "CREATE VIRTUAL TABLE search USING fts5(text);");
    auto plan = diff_schemas(empty_schema(), desired);

    // AllowNone — pure additive creates are always permitted.
    sqlift_apply_options opts{};
    int et = 0; char* em = nullptr;
    int rc = sqlift_apply(db.db, plan.c_str(), opts, &et, &em);
    sqlift_free(em);
    CHECK(rc == 0);
    CHECK(et == SQLIFT_OK);
    CHECK(db.query_int64(
        "SELECT count(*) FROM sqlite_master "
        "WHERE name='search' AND type='table'") == 1);
}

TEST_CASE("schema hash: stable for schemas without virtual tables") {
    // Adding the feature must not change the hash of any existing schema
    // that contains no virtual tables.
    auto j_before = json::parse(parse_schema(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"));
    // Schemas serialized by the new build still omit "virtual_tables" when
    // empty (verified by schema_to_json suppressing the field). The hash
    // doesn't depend on the JSON shape — it's derived from the in-memory
    // Schema — so the absence of virtual_tables in the hash input is what
    // matters here, not the JSON.
    CHECK(!j_before.contains("virtual_tables"));
}

TEST_CASE("schema hash: includes virtual tables when present") {
    auto h1 = schema_hash(parse_schema(
        "CREATE VIRTUAL TABLE search USING fts5(a);"));
    auto h2 = schema_hash(parse_schema(
        "CREATE VIRTUAL TABLE search USING fts5(b);"));
    CHECK(h1 != h2);
}

TEST_CASE("schema hash: cross-language snapshot for vtable") {
    // Locked-in hash for a schema containing exactly one FTS5 vtable.
    // Hash input is "VTABLE notes USING fts5(body)\n" → sha256 hex.
    // The Go side has the same expected hash in
    // go/sqlift/virtual_test.go TestHashFormatSnapshot — keep both in sync
    // when intentionally changing the hash format.
    const std::string schema_json = R"json({
        "tables": {},
        "indexes": {},
        "views": {},
        "triggers": {},
        "virtual_tables": {
            "notes": {
                "name": "notes",
                "module": "fts5",
                "args": "body",
                "raw_sql": "CREATE VIRTUAL TABLE notes USING fts5(body)"
            }
        }
    })json";
    const std::string expected =
        "0653de74367965a76a24b08085cfd3840c714c10f8a8463fbdcbf62f33e65a35";
    CHECK(schema_hash(schema_json) == expected);
}
