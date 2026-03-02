// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

#include "sqlift_c.h"
#include "sqlift.h"

#include <sqlite3.h>
#include <cstdlib>
#include <cstring>
#include <string>

// --- helpers -----------------------------------------------------------------

namespace {

// Duplicate a std::string to a malloc'd C string (caller frees with sqlift_free).
char* dup_str(const std::string& s) {
    char* p = static_cast<char*>(std::malloc(s.size() + 1));
    if (p) std::memcpy(p, s.c_str(), s.size() + 1);
    return p;
}

// Set error output pointers. msg is malloc'd; caller frees with sqlift_free.
void set_error(int* err_type, char** err_msg, int type, const std::string& msg) {
    if (err_type) *err_type = type;
    if (err_msg)  *err_msg = dup_str(msg);
}

void clear_error(int* err_type, char** err_msg) {
    if (err_type) *err_type = SQLIFT_OK;
    if (err_msg)  *err_msg = nullptr;
}

// Map a C++ exception to the error type enum.
int classify_exception(const std::exception& e) {
    if (dynamic_cast<const sqlift::ParseError*>(&e))          return SQLIFT_PARSE_ERROR;
    if (dynamic_cast<const sqlift::ExtractError*>(&e))        return SQLIFT_EXTRACT_ERROR;
    if (dynamic_cast<const sqlift::DiffError*>(&e))           return SQLIFT_DIFF_ERROR;
    if (dynamic_cast<const sqlift::DriftError*>(&e))          return SQLIFT_DRIFT_ERROR;
    if (dynamic_cast<const sqlift::DestructiveError*>(&e))    return SQLIFT_DESTRUCTIVE_ERROR;
    if (dynamic_cast<const sqlift::BreakingChangeError*>(&e)) return SQLIFT_BREAKING_CHANGE_ERROR;
    if (dynamic_cast<const sqlift::JsonError*>(&e))           return SQLIFT_JSON_ERROR;
    if (dynamic_cast<const sqlift::ApplyError*>(&e))          return SQLIFT_APPLY_ERROR;
    if (dynamic_cast<const sqlift::Error*>(&e))               return SQLIFT_ERROR;
    return SQLIFT_ERROR;
}

// Warning JSON serialization (reused by sqlift_diff and sqlift_detect_redundant_indexes).
std::string warnings_to_json(const std::vector<sqlift::Warning>& warnings) {
    std::string s = "[";
    for (size_t i = 0; i < warnings.size(); ++i) {
        if (i > 0) s += ',';
        const auto& w = warnings[i];
        // Manual JSON to avoid pulling nlohmann into this TU via includes.
        // The values are simple strings, no escaping issues in practice.
        s += "{\"type\":\"RedundantIndex\"";
        s += ",\"message\":\"" + w.message + "\"";
        s += ",\"index_name\":\"" + w.index_name + "\"";
        s += ",\"covered_by\":\"" + w.covered_by + "\"";
        s += ",\"table_name\":\"" + w.table_name + "\"}";
    }
    s += "]";
    return s;
}

} // namespace

// --- opaque handle -----------------------------------------------------------

struct sqlift_db {
    sqlift::Database db;
    explicit sqlift_db(const std::string& path, int flags)
        : db(path, flags ? flags : (SQLITE_OPEN_READWRITE | SQLITE_OPEN_CREATE)) {}
};

// --- C API -------------------------------------------------------------------

extern "C" {

sqlift_db* sqlift_db_open(const char* path, int flags,
                          int* err_type, char** err_msg) {
    clear_error(err_type, err_msg);
    try {
        return new sqlift_db(path, flags);
    } catch (const std::exception& e) {
        set_error(err_type, err_msg, classify_exception(e), e.what());
        return nullptr;
    }
}

void sqlift_db_close(sqlift_db* db) {
    delete db;
}

int sqlift_db_exec(sqlift_db* db, const char* sql, char** err_msg) {
    if (err_msg) *err_msg = nullptr;
    try {
        db->db.exec(sql);
        return 0;
    } catch (const std::exception& e) {
        if (err_msg) *err_msg = dup_str(e.what());
        return 1;
    }
}

char* sqlift_parse(const char* ddl, int* err_type, char** err_msg) {
    clear_error(err_type, err_msg);
    try {
        auto schema = sqlift::parse(ddl);
        return dup_str(sqlift::schema_to_json(schema));
    } catch (const std::exception& e) {
        set_error(err_type, err_msg, classify_exception(e), e.what());
        return nullptr;
    }
}

char* sqlift_extract(sqlift_db* db, int* err_type, char** err_msg) {
    clear_error(err_type, err_msg);
    try {
        auto schema = sqlift::extract(db->db);
        return dup_str(sqlift::schema_to_json(schema));
    } catch (const std::exception& e) {
        set_error(err_type, err_msg, classify_exception(e), e.what());
        return nullptr;
    }
}

char* sqlift_diff(const char* current_json, const char* desired_json,
                  int* err_type, char** err_msg) {
    clear_error(err_type, err_msg);
    try {
        auto current = sqlift::schema_from_json(current_json);
        auto desired = sqlift::schema_from_json(desired_json);
        auto plan = sqlift::diff(current, desired);
        // Include warnings in the plan JSON (they're part of to_json output).
        return dup_str(sqlift::to_json(plan));
    } catch (const std::exception& e) {
        set_error(err_type, err_msg, classify_exception(e), e.what());
        return nullptr;
    }
}

int sqlift_apply(sqlift_db* db, const char* plan_json, int allow_destructive,
                 int* err_type, char** err_msg) {
    clear_error(err_type, err_msg);
    try {
        auto plan = sqlift::from_json(plan_json);
        sqlift::ApplyOptions opts;
        opts.allow_destructive = (allow_destructive != 0);
        sqlift::apply(db->db, plan, opts);
        return 0;
    } catch (const std::exception& e) {
        set_error(err_type, err_msg, classify_exception(e), e.what());
        return 1;
    }
}

int64_t sqlift_migration_version(sqlift_db* db, int* err_type, char** err_msg) {
    clear_error(err_type, err_msg);
    try {
        return sqlift::migration_version(db->db);
    } catch (const std::exception& e) {
        set_error(err_type, err_msg, classify_exception(e), e.what());
        return -1;
    }
}

char* sqlift_detect_redundant_indexes(const char* schema_json,
                                      int* err_type, char** err_msg) {
    clear_error(err_type, err_msg);
    try {
        auto schema = sqlift::schema_from_json(schema_json);
        auto warnings = sqlift::detect_redundant_indexes(schema);
        return dup_str(warnings_to_json(warnings));
    } catch (const std::exception& e) {
        set_error(err_type, err_msg, classify_exception(e), e.what());
        return nullptr;
    }
}

char* sqlift_schema_hash(const char* schema_json,
                         int* err_type, char** err_msg) {
    clear_error(err_type, err_msg);
    try {
        auto schema = sqlift::schema_from_json(schema_json);
        return dup_str(schema.hash());
    } catch (const std::exception& e) {
        set_error(err_type, err_msg, classify_exception(e), e.what());
        return nullptr;
    }
}

int sqlift_db_query_int64(sqlift_db* db, const char* sql,
                          int64_t* result, char** err_msg) {
    if (err_msg) *err_msg = nullptr;
    try {
        sqlite3_stmt* stmt = nullptr;
        int rc = sqlite3_prepare_v2(db->db.get(), sql, -1, &stmt, nullptr);
        if (rc != SQLITE_OK) {
            if (err_msg) *err_msg = dup_str(sqlite3_errmsg(db->db.get()));
            return 1;
        }
        rc = sqlite3_step(stmt);
        if (rc == SQLITE_ROW) {
            if (result) *result = sqlite3_column_int64(stmt, 0);
            sqlite3_finalize(stmt);
            return 0;
        }
        sqlite3_finalize(stmt);
        if (rc == SQLITE_DONE) {
            // No rows — return 0 as default.
            if (result) *result = 0;
            return 0;
        }
        if (err_msg) *err_msg = dup_str(sqlite3_errmsg(db->db.get()));
        return 1;
    } catch (const std::exception& e) {
        if (err_msg) *err_msg = dup_str(e.what());
        return 1;
    }
}

char* sqlift_db_query_text(sqlift_db* db, const char* sql, char** err_msg) {
    if (err_msg) *err_msg = nullptr;
    try {
        sqlite3_stmt* stmt = nullptr;
        int rc = sqlite3_prepare_v2(db->db.get(), sql, -1, &stmt, nullptr);
        if (rc != SQLITE_OK) {
            if (err_msg) *err_msg = dup_str(sqlite3_errmsg(db->db.get()));
            return nullptr;
        }
        rc = sqlite3_step(stmt);
        if (rc == SQLITE_ROW) {
            const char* text = reinterpret_cast<const char*>(sqlite3_column_text(stmt, 0));
            char* result = dup_str(text ? text : "");
            sqlite3_finalize(stmt);
            return result;
        }
        sqlite3_finalize(stmt);
        if (rc == SQLITE_DONE) {
            // No rows — return empty string.
            return dup_str("");
        }
        if (err_msg) *err_msg = dup_str(sqlite3_errmsg(db->db.get()));
        return nullptr;
    } catch (const std::exception& e) {
        if (err_msg) *err_msg = dup_str(e.what());
        return nullptr;
    }
}

void sqlift_free(void* ptr) {
    std::free(ptr);
}

} // extern "C"
