include std/cxx.mk

cxx ?= c++
cxxflags = -std=c++23 -Wall -Wextra -Wpedantic -O2
cppflags = -Idist -Ivendor/include
ldlibs = -lsqlite3
prefix ?= /usr/local

test_src = $[wildcard tests/*.cpp]
test_obj = $[patsubst tests/%.cpp,build/test/%.o,$test_src]

lib = build/libsqlift.a
test_bin = build/test_sqlift

san_cxxflags = -std=c++23 -Wall -Wextra -Wpedantic -g -O1 -fsanitize=address,undefined
san_obj = $[patsubst tests/%.cpp,build/sanitize/test/%.o,$test_src]
san_bin = build/sanitize/test_sqlift

$lib: build/sqlift.o build/sqlift_c.o
    ar rcs $target $inputs

build/sqlift.o: dist/sqlift.cpp dist/sqlift.h
    $cxx $cxxflags $cppflags -c $input -o $target

build/sqlift_c.o: dist/sqlift_c.cpp dist/sqlift_c.h dist/sqlift.h
    $cxx $cxxflags $cppflags -c $input -o $target

build/test/{name}.o: tests/{name}.cpp dist/sqlift.h
    $cxx $cxxflags $cppflags -c $input -o $target

!lib: $lib

!test: $test_bin
    ./$input

$test_bin: $test_obj $lib
    $cxx $cxxflags $inputs $ldlibs -o $target

build/sanitize/sqlift.o: dist/sqlift.cpp dist/sqlift.h
    $cxx $san_cxxflags $cppflags -c $input -o $target

build/sanitize/sqlift_c.o: dist/sqlift_c.cpp dist/sqlift_c.h dist/sqlift.h
    $cxx $san_cxxflags $cppflags -c $input -o $target

build/sanitize/test/{name}.o: tests/{name}.cpp dist/sqlift.h
    $cxx $san_cxxflags $cppflags -c $input -o $target

build/sanitize/libsqlift.a: build/sanitize/sqlift.o build/sanitize/sqlift_c.o
    ar rcs $target $inputs

$san_bin: $san_obj build/sanitize/libsqlift.a
    $cxx $san_cxxflags $inputs $ldlibs -o $target

!sanitize: $san_bin
    ./$input

!install: $lib
    mkdir -p $prefix/include $prefix/lib
    cp dist/sqlift.h dist/sqlift_c.h $prefix/include/
    cp $lib $prefix/lib/

!uninstall:
    rm -f $prefix/include/sqlift.h $prefix/include/sqlift_c.h $prefix/lib/libsqlift.a

!clean:
    rm -rf build/ .mk/
