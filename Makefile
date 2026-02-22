MAKEFLAGS += -j$(shell sysctl -n hw.ncpu 2>/dev/null || nproc 2>/dev/null || echo 4)

CXX      ?= c++
CXXFLAGS := -std=c++23 -Wall -Wextra -Wpedantic -O2
CPPFLAGS := -I. -I$(shell brew --prefix doctest 2>/dev/null)/include
LDLIBS   := -lsqlite3

TEST_DIR  := tests
TEST_SRCS := $(wildcard $(TEST_DIR)/*.cpp)
TEST_OBJS := $(TEST_SRCS:$(TEST_DIR)/%.cpp=build/test/%.o)

LIB      := build/libsqlift.a
TEST_BIN := build/test_sqlift

.PHONY: all lib test clean

all: lib test

lib: $(LIB)

$(LIB): build/sqlift.o
	@mkdir -p $(@D)
	$(AR) rcs $@ $^

build/sqlift.o: sqlift.cpp sqlift.h
	@mkdir -p $(@D)
	$(CXX) $(CXXFLAGS) $(CPPFLAGS) -MMD -MP -c $< -o $@

build/test/%.o: $(TEST_DIR)/%.cpp sqlift.h
	@mkdir -p $(@D)
	$(CXX) $(CXXFLAGS) $(CPPFLAGS) -MMD -MP -c $< -o $@

test: $(TEST_BIN)
	./$(TEST_BIN)

$(TEST_BIN): $(TEST_OBJS) $(LIB)
	@mkdir -p $(@D)
	$(CXX) $(CXXFLAGS) $^ $(LDLIBS) -o $@

clean:
	rm -rf build

-include $(wildcard build/*.d build/test/*.d)
