// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

#include <doctest/doctest.h>
#include "sqlift.h"

using namespace sqlift;

TEST_CASE("to_string covers all OpType values") {
    CHECK(to_string(OpType::CreateTable)   == "CreateTable");
    CHECK(to_string(OpType::DropTable)     == "DropTable");
    CHECK(to_string(OpType::RebuildTable)  == "RebuildTable");
    CHECK(to_string(OpType::AddColumn)     == "AddColumn");
    CHECK(to_string(OpType::CreateIndex)   == "CreateIndex");
    CHECK(to_string(OpType::DropIndex)     == "DropIndex");
    CHECK(to_string(OpType::CreateView)    == "CreateView");
    CHECK(to_string(OpType::DropView)      == "DropView");
    CHECK(to_string(OpType::CreateTrigger) == "CreateTrigger");
    CHECK(to_string(OpType::DropTrigger)   == "DropTrigger");
}

TEST_CASE("op_type_from_string round-trips with to_string") {
    for (auto t : {OpType::CreateTable, OpType::DropTable, OpType::RebuildTable,
                   OpType::AddColumn, OpType::CreateIndex, OpType::DropIndex,
                   OpType::CreateView, OpType::DropView,
                   OpType::CreateTrigger, OpType::DropTrigger}) {
        CHECK(op_type_from_string(to_string(t)) == t);
    }
}

TEST_CASE("op_type_from_string rejects unknown strings") {
    CHECK_THROWS_AS(op_type_from_string("NotAnOp"), JsonError);
    CHECK_THROWS_AS(op_type_from_string(""), JsonError);
    CHECK_THROWS_AS(op_type_from_string("createtable"), JsonError);
}

TEST_CASE("json round-trip: empty plan") {
    Schema s = parse("CREATE TABLE t (id INTEGER PRIMARY KEY);");
    auto plan = diff(s, s);
    CHECK(plan.empty());

    std::string json = to_json(plan);
    auto restored = from_json(json);
    CHECK(restored.empty());
    CHECK(restored.operations().size() == 0);
}

TEST_CASE("json round-trip: plan with multiple operation types") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE TABLE old_logs (id INTEGER PRIMARY KEY);"
        "CREATE INDEX idx_name ON users(name);"
        "CREATE VIEW all_users AS SELECT * FROM users;");

    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);"
        "CREATE TABLE posts (id INTEGER PRIMARY KEY, title TEXT);"
        "CREATE VIEW active_users AS SELECT * FROM users;");

    auto plan = diff(current, desired);
    REQUIRE(!plan.empty());

    std::string json = to_json(plan);
    auto restored = from_json(json);

    REQUIRE(restored.operations().size() == plan.operations().size());
    for (size_t i = 0; i < plan.operations().size(); ++i) {
        const auto& orig = plan.operations()[i];
        const auto& rest = restored.operations()[i];
        CHECK(rest.type == orig.type);
        CHECK(rest.object_name == orig.object_name);
        CHECK(rest.description == orig.description);
        CHECK(rest.sql == orig.sql);
        CHECK(rest.destructive == orig.destructive);
    }
}

TEST_CASE("json round-trip: destructive operations") {
    Schema current = parse("CREATE TABLE users (id INTEGER PRIMARY KEY);");
    Schema desired;

    auto plan = diff(current, desired);
    CHECK(plan.has_destructive_operations());

    std::string json = to_json(plan);
    auto restored = from_json(json);
    CHECK(restored.has_destructive_operations());
    REQUIRE(restored.operations().size() == 1);
    CHECK(restored.operations()[0].destructive == true);
    CHECK(restored.operations()[0].type == OpType::DropTable);
}

TEST_CASE("json round-trip: rebuild table preserves multi-statement sql") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, age TEXT);");
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, age INTEGER);");

    auto plan = diff(current, desired);
    REQUIRE(plan.operations().size() == 1);
    CHECK(plan.operations()[0].type == OpType::RebuildTable);
    CHECK(plan.operations()[0].sql.size() > 1);

    std::string json = to_json(plan);
    auto restored = from_json(json);
    CHECK(restored.operations()[0].sql == plan.operations()[0].sql);
}

TEST_CASE("deserialized plan can be applied to a database") {
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);"
        "CREATE INDEX idx_name ON users(name);");

    auto plan = diff(Schema{}, desired);
    std::string json = to_json(plan);
    auto restored = from_json(json);

    Database db(":memory:");
    apply(db, restored);

    Schema after = extract(db);
    CHECK(after.tables.count("users"));
    CHECK(after.tables.at("users").columns.size() == 2);
    CHECK(after.indexes.count("idx_name"));
}

TEST_CASE("from_json rejects invalid JSON") {
    CHECK_THROWS_AS(from_json("not json"), JsonError);
    CHECK_THROWS_AS(from_json(""), JsonError);
}

TEST_CASE("from_json rejects non-object top level") {
    CHECK_THROWS_AS(from_json("[1,2,3]"), JsonError);
    CHECK_THROWS_AS(from_json("42"), JsonError);
}

TEST_CASE("from_json rejects missing version") {
    CHECK_THROWS_AS(from_json(R"({"operations":[]})"), JsonError);
}

TEST_CASE("from_json rejects unsupported version") {
    CHECK_THROWS_AS(from_json(R"({"version":999,"operations":[]})"), JsonError);
}

TEST_CASE("from_json rejects missing operations") {
    CHECK_THROWS_AS(from_json(R"({"version":1})"), JsonError);
}

TEST_CASE("from_json rejects operation with missing fields") {
    CHECK_THROWS_AS(from_json(R"({"version":1,"operations":[
        {"object_name":"t","description":"d","sql":["s"],"destructive":false}
    ]})"), JsonError);

    CHECK_THROWS_AS(from_json(R"({"version":1,"operations":[
        {"type":"CreateTable","object_name":"t","description":"d","destructive":false}
    ]})"), JsonError);
}

TEST_CASE("from_json rejects unknown OpType string") {
    CHECK_THROWS_AS(from_json(R"({"version":1,"operations":[
        {"type":"Bogus","object_name":"t","description":"d","sql":[],"destructive":false}
    ]})"), JsonError);
}
