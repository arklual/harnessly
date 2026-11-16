#!/usr/bin/env bash
# Rebuilds internal/design/vendor/react19.production.min.js: React 19 has no UMD build, so react, react-dom and
# react-dom/client are bundled by esbuild into one script that sets window.React and window.ReactDOM.
# Then update the SRI in internal/design/vendor.go (TestVendorFilesMatchEngineSRI prints the right one).
set -euo pipefail
v=${1:-19.2.0}
d=$(mktemp -d); cd "$d"
npm init -y >/dev/null; npm i "react@$v" "react-dom@$v" esbuild >/dev/null
cat > entry.js <<'JS'
import * as R from "react";
import * as D from "react-dom";
import * as C from "react-dom/client";
window.React = R;
window.ReactDOM = Object.assign({}, D, C);
JS
npx esbuild entry.js --bundle --minify --format=iife '--define:process.env.NODE_ENV="production"' --outfile=out.js
cp out.js "$OLDPWD/internal/design/vendor/react19.production.min.js"
