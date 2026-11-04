// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
//
// Live Postgres extract / parse-on / diff / apply. Built with
// -DSQLIFT_HAVE_POSTGRES when libpq is available. Without a server the
// suite skips so `cv test` stays green locally.

#include <doctest/doctest.h>
#include "sqlift.h"
#include "test_helpers.h"

#include <cstdlib>
#include <cstring>
#include <string>
#include <vector>

#ifndef SQLIFT_HAVE_POSTGRES

TEST_CASE("postgres extract/parse-on/apply") {
    MESSAGE("SQLIFT_HAVE_POSTGRES not set — live Postgres suite skipped");
}

#else

#include <libpq-fe.h>

namespace {

const char* conninfo() {
    if (const char* e = std::getenv("SQLIFT_PG"); e && *e) return e;
    return "";
}

int pq_exec(void* ctx, const char* sql, char** err_msg) {
    auto* c = static_cast<PGconn*>(ctx);
    PGresult* r = PQexec(c, sql);
    auto st = PQresultStatus(r);
    if (st != PGRES_COMMAND_OK && st != PGRES_TUPLES_OK) {
        if (err_msg) *err_msg = strdup(PQresultErrorMessage(r));
        PQclear(r);
        return 1;
    }
    PQclear(r);
    return 0;
}

int pq_query(void* ctx, const char* sql,
             void (*row)(void* row_ctx, int ncols, const char* const* cols),
             void* row_ctx, char** err_msg) {
    auto* c = static_cast<PGconn*>(ctx);
    PGresult* r = PQexec(c, sql);
    if (PQresultStatus(r) != PGRES_TUPLES_OK) {
        if (err_msg) *err_msg = strdup(PQresultErrorMessage(r));
        PQclear(r);
        return 1;
    }
    int nt = PQntuples(r);
    int nc = PQnfields(r);
    std::vector<const char*> cols(static_cast<size_t>(nc));
    for (int i = 0; i < nt; ++i) {
        for (int j = 0; j < nc; ++j)
            cols[static_cast<size_t>(j)] =
                PQgetisnull(r, i, j) ? nullptr : PQgetvalue(r, i, j);
        row(row_ctx, nc, cols.data());
    }
    PQclear(r);
    return 0;
}

struct Pg {
    PGconn* conn = nullptr;
    sqlift_db* db = nullptr;
    explicit Pg() {
        conn = PQconnectdb(conninfo());
        if (PQstatus(conn) != CONNECTION_OK) {
            std::string msg = PQerrorMessage(conn);
            PQfinish(conn);
            conn = nullptr;
            throw std::runtime_error("PQconnectdb: " + msg);
        }
        sqlift_exec ex{conn, pq_exec, pq_query};
        int et = 0;
        char* em = nullptr;
        db = sqlift_db_wrap_exec(SQLIFT_ENGINE_POSTGRES, ex, &et, &em);
        if (!db) {
            std::string msg = em ? em : "wrap failed";
            sqlift_free(em);
            PQfinish(conn);
            conn = nullptr;
            throw std::runtime_error(msg);
        }
    }
    ~Pg() {
        sqlift_db_close(db);
        if (conn) PQfinish(conn);
    }
    Pg(const Pg&) = delete;
    Pg& operator=(const Pg&) = delete;

    void reset() {
        char* em = nullptr;
        sqlift_db_exec(db, "DROP SCHEMA IF EXISTS public CASCADE", &em);
        sqlift_free(em);
        sqlift_db_exec(db, "DROP SCHEMA IF EXISTS sqlpipe CASCADE", &em);
        sqlift_free(em);
        sqlift_db_exec(db, "CREATE SCHEMA public", &em);
        sqlift_free(em);
        sqlift_db_exec(db, "GRANT ALL ON SCHEMA public TO PUBLIC", &em);
        sqlift_free(em);
    }
};

json parse_on(sqlift_db* db, const char* ddl) {
    int et = 0;
    char* em = nullptr;
    CStr s(sqlift_parse_on(db, ddl, &et, &em));
    if (!s) {
        std::string msg = em ? em : "parse_on failed";
        sqlift_free(em);
        throw std::runtime_error(msg);
    }
    return json::parse(s.str());
}

json extract_json(sqlift_db* db) {
    int et = 0;
    char* em = nullptr;
    CStr s(sqlift_extract(db, &et, &em));
    if (!s) {
        std::string msg = em ? em : "extract failed";
        sqlift_free(em);
        throw std::runtime_error(msg);
    }
    return json::parse(s.str());
}

} // namespace

TEST_CASE("postgres extract/parse-on/apply") {
    PGconn* probe = PQconnectdb(conninfo());
    if (PQstatus(probe) != CONNECTION_OK) {
        std::string msg = PQerrorMessage(probe);
        PQfinish(probe);
        MESSAGE("no Postgres server — live suite skipped: " << msg);
        return;
    }
    PQfinish(probe);

    Pg pg;
    pg.reset();

    SUBCASE("extract tables/columns/pk/fk/check/index/view/trigger") {
        REQUIRE(sqlift_db_exec(pg.db,
            "CREATE TABLE users ("
            "  id BIGINT PRIMARY KEY,"
            "  name TEXT NOT NULL,"
            "  email TEXT,"
            "  CONSTRAINT email_len CHECK (char_length(email) < 200)"
            ");"
            "CREATE TABLE orders ("
            "  id BIGINT PRIMARY KEY,"
            "  user_id BIGINT NOT NULL REFERENCES users(id),"
            "  total INTEGER"
            ");"
            "CREATE INDEX idx_orders_user ON orders(user_id);"
            "CREATE VIEW user_names AS SELECT id, name FROM users;"
            "CREATE FUNCTION sqlift_tg() RETURNS trigger AS $$"
            "BEGIN NEW.name := NEW.name; RETURN NEW; END; $$ LANGUAGE plpgsql;"
            "CREATE TRIGGER users_tg BEFORE INSERT ON users "
            "FOR EACH ROW EXECUTE FUNCTION sqlift_tg();",
            nullptr) == 0);

        auto schema = extract_json(pg.db);
        CHECK(schema["dialect"] == "postgres");
        REQUIRE(schema["tables"].contains("users"));
        REQUIRE(schema["tables"].contains("orders"));
        auto users = schema["tables"]["users"];
        CHECK(users["columns"].size() >= 3);
        bool saw_pk = false;
        for (const auto& c : users["columns"])
            if (c["name"] == "id" && c["pk"].get<int>() > 0) saw_pk = true;
        CHECK(saw_pk);
        REQUIRE_FALSE(schema["tables"]["orders"]["foreign_keys"].empty());
        CHECK(schema["tables"]["users"]["check_constraints"].size() >= 1);
        CHECK(schema["indexes"].contains("idx_orders_user"));
        CHECK(schema["views"].contains("user_names"));
        CHECK(schema["triggers"].contains("users_tg"));
    }

    SUBCASE("parse_on temp schema") {
        auto schema = parse_on(pg.db,
            "CREATE TABLE t (id BIGINT PRIMARY KEY, name TEXT)");
        CHECK(schema["dialect"] == "postgres");
        REQUIRE(schema["tables"].contains("t"));
        auto live = extract_json(pg.db);
        CHECK_FALSE(live["tables"].contains("t"));
    }

    SUBCASE("additive ADD COLUMN + drift hash") {
        auto desired = parse_on(pg.db,
            "CREATE TABLE items (id BIGINT PRIMARY KEY, name TEXT)");
        auto current = extract_json(pg.db);
        int et = 0;
        char* em = nullptr;
        CStr plan(sqlift_diff(current.dump().c_str(), desired.dump().c_str(),
                              &et, &em));
        REQUIRE(plan);
        REQUIRE(sqlift_apply(pg.db, plan.ptr, sqlift_apply_options{},
                             &et, &em) == 0);

        auto after = extract_json(pg.db);
        REQUIRE(after["tables"].contains("items"));

        auto desired2 = parse_on(pg.db,
            "CREATE TABLE items (id BIGINT PRIMARY KEY, name TEXT, note TEXT)");
        CStr plan2(sqlift_diff(after.dump().c_str(), desired2.dump().c_str(),
                               &et, &em));
        REQUIRE(plan2);
        auto planj = json::parse(plan2.str());
        REQUIRE_FALSE(planj["operations"].empty());
        CHECK(planj["operations"][0]["type"] == "AddColumn");
        for (const auto& op : planj["operations"])
            CHECK(op["type"] != "RebuildTable");

        REQUIRE(sqlift_apply(pg.db, plan2.ptr, sqlift_apply_options{},
                             &et, &em) == 0);
        auto after2 = extract_json(pg.db);
        bool saw_note = false;
        for (const auto& c : after2["tables"]["items"]["columns"])
            if (c["name"] == "note") saw_note = true;
        CHECK(saw_note);

        int64_t ver = sqlift_migration_version(pg.db, &et, &em);
        CHECK(ver >= 1);
    }

    SUBCASE("SET NOT NULL rejected without ALLOW_DATA_DEPENDENT") {
        auto desired = parse_on(pg.db,
            "CREATE TABLE n (id BIGINT PRIMARY KEY, name TEXT)");
        auto current = extract_json(pg.db);
        int et = 0;
        char* em = nullptr;
        CStr plan(sqlift_diff(current.dump().c_str(), desired.dump().c_str(),
                              &et, &em));
        REQUIRE(sqlift_apply(pg.db, plan.ptr, sqlift_apply_options{},
                             &et, &em) == 0);

        auto tight = parse_on(pg.db,
            "CREATE TABLE n (id BIGINT PRIMARY KEY, name TEXT NOT NULL)");
        auto now = extract_json(pg.db);
        CStr plan2(sqlift_diff(now.dump().c_str(), tight.dump().c_str(),
                               &et, &em));
        REQUIRE(plan2);
        int rc = sqlift_apply(pg.db, plan2.ptr, sqlift_apply_options{},
                              &et, &em);
        CHECK(rc != 0);
        CHECK(et == SQLIFT_BREAKING_CHANGE_ERROR);
        sqlift_free(em);
    }
}

#endif // SQLIFT_HAVE_POSTGRES
