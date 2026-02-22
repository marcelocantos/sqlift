#include <doctest/doctest.h>
#include "sqlift.h"

using namespace sqlift;

TEST_CASE("diff identical schemas") {
    auto sql = "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);";
    Schema a = parse(sql);
    Schema b = parse(sql);
    auto plan = diff(a, b);
    CHECK(plan.empty());
}

TEST_CASE("diff add table") {
    Schema empty;
    Schema desired = parse("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");

    auto plan = diff(empty, desired);
    REQUIRE(plan.operations().size() == 1);
    CHECK(plan.operations()[0].type == OpType::CreateTable);
    CHECK(plan.operations()[0].object_name == "users");
    CHECK(!plan.has_destructive_operations());
}

TEST_CASE("diff drop table") {
    Schema current = parse("CREATE TABLE users (id INTEGER PRIMARY KEY);");
    Schema empty;

    auto plan = diff(current, empty);
    REQUIRE(plan.operations().size() == 1);
    CHECK(plan.operations()[0].type == OpType::DropTable);
    CHECK(plan.operations()[0].destructive == true);
}

TEST_CASE("diff add column - simple append") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);");

    auto plan = diff(current, desired);
    REQUIRE(plan.operations().size() == 1);
    CHECK(plan.operations()[0].type == OpType::AddColumn);
    CHECK(plan.operations()[0].object_name == "users");
    CHECK(!plan.has_destructive_operations());
}

TEST_CASE("diff add NOT NULL column with default - simple append") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY);");
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, active INTEGER NOT NULL DEFAULT 1);");

    auto plan = diff(current, desired);
    REQUIRE(plan.operations().size() == 1);
    CHECK(plan.operations()[0].type == OpType::AddColumn);
}

TEST_CASE("diff add NOT NULL column without default - requires rebuild") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY);");
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);");

    auto plan = diff(current, desired);
    REQUIRE(plan.operations().size() == 1);
    CHECK(plan.operations()[0].type == OpType::RebuildTable);
}

TEST_CASE("diff remove column") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT);");
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");

    auto plan = diff(current, desired);
    REQUIRE(plan.operations().size() == 1);
    CHECK(plan.operations()[0].type == OpType::RebuildTable);
    CHECK(plan.operations()[0].destructive == true);
}

TEST_CASE("diff change column type") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, age TEXT);");
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, age INTEGER);");

    auto plan = diff(current, desired);
    REQUIRE(plan.operations().size() == 1);
    CHECK(plan.operations()[0].type == OpType::RebuildTable);
}

TEST_CASE("diff change column nullability") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);");

    auto plan = diff(current, desired);
    REQUIRE(plan.operations().size() == 1);
    CHECK(plan.operations()[0].type == OpType::RebuildTable);
}

TEST_CASE("diff add index") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);");
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);"
        "CREATE INDEX idx_email ON users(email);");

    auto plan = diff(current, desired);
    REQUIRE(plan.operations().size() == 1);
    CHECK(plan.operations()[0].type == OpType::CreateIndex);
}

TEST_CASE("diff drop index") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);"
        "CREATE INDEX idx_email ON users(email);");
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT);");

    auto plan = diff(current, desired);
    REQUIRE(plan.operations().size() == 1);
    CHECK(plan.operations()[0].type == OpType::DropIndex);
    CHECK(plan.operations()[0].destructive == true);
}

TEST_CASE("diff add view") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);");
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"
        "CREATE VIEW all_users AS SELECT * FROM users;");

    auto plan = diff(current, desired);
    REQUIRE(plan.operations().size() == 1);
    CHECK(plan.operations()[0].type == OpType::CreateView);
}

TEST_CASE("diff change view") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, active INTEGER);"
        "CREATE VIEW all_users AS SELECT * FROM users;");
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, active INTEGER);"
        "CREATE VIEW all_users AS SELECT * FROM users WHERE active = 1;");

    auto plan = diff(current, desired);
    // Should drop then create the view
    bool has_drop = false, has_create = false;
    for (const auto& op : plan.operations()) {
        if (op.type == OpType::DropView) has_drop = true;
        if (op.type == OpType::CreateView) has_create = true;
    }
    CHECK(has_drop);
    CHECK(has_create);
}

TEST_CASE("diff add trigger") {
    Schema current = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY);"
        "CREATE TABLE log (msg TEXT);");
    Schema desired = parse(
        "CREATE TABLE users (id INTEGER PRIMARY KEY);"
        "CREATE TABLE log (msg TEXT);"
        "CREATE TRIGGER on_insert AFTER INSERT ON users "
        "BEGIN INSERT INTO log VALUES ('added'); END;");

    auto plan = diff(current, desired);
    REQUIRE(plan.operations().size() == 1);
    CHECK(plan.operations()[0].type == OpType::CreateTrigger);
}

TEST_CASE("diff destructive guard") {
    Schema current = parse("CREATE TABLE users (id INTEGER PRIMARY KEY);");
    Schema empty;
    auto plan = diff(current, empty);
    CHECK(plan.has_destructive_operations());
}
