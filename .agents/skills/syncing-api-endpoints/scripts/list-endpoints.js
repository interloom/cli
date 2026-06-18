#!/usr/bin/env node
"use strict";

// List the public API surface of an OpenAPI spec, one line per operation.
//
// Usage:
//   list-endpoints.js [SPEC]      # SPEC defaults to internal/api/openapi.json
//   list-endpoints.js -           # read the spec from stdin
//
// Each line is "METHOD /path  [query: a, b, c]" so the output diffs cleanly.
// Compare the committed spec against the freshly generated one to see exactly
// which endpoints and query parameters were added, removed, or changed:
//
//   git show HEAD:internal/api/openapi.json | list-endpoints.js - > /tmp/before.txt
//   list-endpoints.js internal/api/openapi.json                  > /tmp/after.txt
//   diff /tmp/before.txt /tmp/after.txt

const fs = require("fs");

const PREFIX = "/api/v1/public";
const METHODS = new Set(["get", "post", "put", "patch", "delete"]);

function readInput(arg) {
  if (arg === "-") return fs.readFileSync(0, "utf8");
  return fs.readFileSync(arg, "utf8");
}

function main() {
  const arg = process.argv[2] || "internal/api/openapi.json";
  const spec = JSON.parse(readInput(arg));

  const lines = [];
  for (const [path, item] of Object.entries(spec.paths || {})) {
    const short = path.startsWith(PREFIX) ? path.slice(PREFIX.length) : path;
    for (const [method, op] of Object.entries(item)) {
      if (!METHODS.has(method.toLowerCase())) continue;
      const params = (op.parameters || [])
        .filter((p) => p.in === "query")
        .map((p) => p.name)
        .sort();
      const suffix = params.length ? `  [query: ${params.join(", ")}]` : "";
      lines.push(`${method.toUpperCase().padEnd(6)} ${short}${suffix}`);
    }
  }

  process.stdout.write(lines.sort().join("\n") + "\n");
}

main();
