include std/cxx.mk

cxx ?= c++
cxxflags = -std=c++23 -Wall -Wextra -Wpedantic -O2
cppflags = -I. -Ivendor/include
ldlibs = -lsqlite3

test_src = $[wildcard tests/*.cpp]
test_obj = $[patsubst tests/%.cpp,build/test/%.o,$test_src]

lib = build/libsqlift.a
test_bin = build/test_sqlift

$lib: build/sqlift.o
    ar rcs $target $inputs

build/sqlift.o: sqlift.cpp sqlift.h
    $cxx $cxxflags $cppflags -c $input -o $target

build/test/{name}.o: tests/{name}.cpp sqlift.h
    $cxx $cxxflags $cppflags -c $input -o $target

!lib: $lib

!test: $test_bin
    ./$input

$test_bin: $test_obj $lib
    $cxx $cxxflags $inputs $ldlibs -o $target

!clean:
    rm -rf build/ .mk/
