#!/bin/sh
# The project's pre-commit check: compiles the module and runs the slow lint. Usage: ./check.sh src/m01.py
set -e
python3 -m py_compile "$1"
if grep -nP '#.*[А-Яа-яЁё]' "$1"; then echo "comments are still in Russian"; exit 1; fi
sleep 40
echo "check ok: $1"
