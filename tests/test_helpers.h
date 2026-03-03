// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

#pragma once

// Test utilities wrapping the C API with RAII and JSON helpers.

#include "sqlift.h"

#include <cstdint>
#include <stdexcept>
#include <string>

#include <nlohmann/json.hpp>

using json = nlohmann::json;

// RAII wrapper for sqlift_db*.
struct TestDB {
    sqlift_db* db;

    explicit TestDB(const char* path = ":memory:", int flags = 0) : db(nullptr) {
        int et; char* em;
        db = sqlift_db_open(path, flags, &et, &em);
        if (!db) {
            std::string msg = em ? em : "unknown error";
            sqlift_free(em);
            throw std::runtime_error("sqlift_db_open: " + msg);
        }
    }

    ~TestDB() { sqlift_db_close(db); }

    void exec(const char* sql) {
        char* em;
        if (sqlift_db_exec(db, sql, &em) != 0) {
            std::string msg = em ? em : "unknown error";
            sqlift_free(em);
            throw std::runtime_error("sqlift_db_exec: " + msg);
        }
    }

    int64_t query_int64(const char* sql) {
        int64_t result = 0;
        char* em;
        if (sqlift_db_query_int64(db, sql, &result, &em) != 0) {
            std::string msg = em ? em : "unknown error";
            sqlift_free(em);
            throw std::runtime_error("sqlift_db_query_int64: " + msg);
        }
        return result;
    }

    std::string query_text(const char* sql) {
        char* em;
        char* result = sqlift_db_query_text(db, sql, &em);
        if (!result && em) {
            std::string msg = em;
            sqlift_free(em);
            throw std::runtime_error("sqlift_db_query_text: " + msg);
        }
        std::string s = result ? result : "";
        sqlift_free(result);
        return s;
    }

    TestDB(const TestDB&) = delete;
    TestDB& operator=(const TestDB&) = delete;
};

// RAII wrapper for malloc'd C strings.
struct CStr {
    char* ptr;
    explicit CStr(char* p = nullptr) : ptr(p) {}
    ~CStr() { sqlift_free(ptr); }
    std::string str() const { return ptr ? ptr : ""; }
    explicit operator bool() const { return ptr != nullptr; }
    CStr(const CStr&) = delete;
    CStr& operator=(const CStr&) = delete;
};

// --- Convenience wrappers (throw on error) ---

inline std::string parse_schema(const char* ddl) {
    int et; char* em;
    CStr result(sqlift_parse(ddl, &et, &em));
    if (et != SQLIFT_OK) {
        std::string msg = em ? em : "unknown error";
        sqlift_free(em);
        throw std::runtime_error("sqlift_parse: " + msg);
    }
    return result.str();
}

inline std::string extract_schema(sqlift_db* db) {
    int et; char* em;
    CStr result(sqlift_extract(db, &et, &em));
    if (et != SQLIFT_OK) {
        std::string msg = em ? em : "unknown error";
        sqlift_free(em);
        throw std::runtime_error("sqlift_extract: " + msg);
    }
    return result.str();
}

inline std::string diff_schemas(const std::string& current, const std::string& desired) {
    int et; char* em;
    CStr result(sqlift_diff(current.c_str(), desired.c_str(), &et, &em));
    if (et != SQLIFT_OK) {
        std::string msg = em ? em : "unknown error";
        sqlift_free(em);
        throw std::runtime_error("sqlift_diff: " + msg);
    }
    return result.str();
}

inline void apply_plan(sqlift_db* db, const std::string& plan_json, bool allow_destructive = false) {
    int et; char* em;
    if (sqlift_apply(db, plan_json.c_str(), allow_destructive ? 1 : 0, &et, &em) != 0) {
        std::string msg = em ? em : "unknown error";
        sqlift_free(em);
        throw std::runtime_error("sqlift_apply: " + msg);
    }
}

inline int64_t migration_ver(sqlift_db* db) {
    int et; char* em;
    int64_t v = sqlift_migration_version(db, &et, &em);
    if (et != SQLIFT_OK) {
        std::string msg = em ? em : "unknown error";
        sqlift_free(em);
        throw std::runtime_error("sqlift_migration_version: " + msg);
    }
    return v;
}

inline std::string schema_hash(const std::string& schema_json) {
    int et; char* em;
    CStr result(sqlift_schema_hash(schema_json.c_str(), &et, &em));
    if (et != SQLIFT_OK) {
        std::string msg = em ? em : "unknown error";
        sqlift_free(em);
        throw std::runtime_error("sqlift_schema_hash: " + msg);
    }
    return result.str();
}

inline json detect_redundant(const std::string& schema_json) {
    int et; char* em;
    CStr result(sqlift_detect_redundant_indexes(schema_json.c_str(), &et, &em));
    if (et != SQLIFT_OK) {
        std::string msg = em ? em : "unknown error";
        sqlift_free(em);
        throw std::runtime_error("sqlift_detect_redundant_indexes: " + msg);
    }
    return json::parse(result.str());
}

// --- Error-returning variants (for testing expected failures) ---

inline int parse_err(const char* ddl) {
    int et; char* em;
    CStr result(sqlift_parse(ddl, &et, &em));
    sqlift_free(em);
    return et;
}

inline int diff_err(const std::string& current, const std::string& desired) {
    int et; char* em;
    CStr result(sqlift_diff(current.c_str(), desired.c_str(), &et, &em));
    sqlift_free(em);
    return et;
}

inline int apply_err(sqlift_db* db, const std::string& plan_json, bool allow_destructive = false) {
    int et; char* em;
    sqlift_apply(db, plan_json.c_str(), allow_destructive ? 1 : 0, &et, &em);
    sqlift_free(em);
    return et;
}

inline int apply_err_msg(sqlift_db* db, const std::string& plan_json,
                         std::string& msg, bool allow_destructive = false) {
    int et; char* em;
    sqlift_apply(db, plan_json.c_str(), allow_destructive ? 1 : 0, &et, &em);
    msg = em ? em : "";
    sqlift_free(em);
    return et;
}

inline std::string empty_schema() {
    return R"({"tables":{},"indexes":{},"views":{},"triggers":{}})";
}
