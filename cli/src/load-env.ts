/**
 * Loads the repo root .env into process.env. Import this first (for its
 * side effect) in any entry point that reads process.env directly, since
 * nothing else in this package does .env loading on its own -- values
 * already set in the shell/environment always take priority.
 */

import * as path from "node:path";
import { config } from "dotenv";

config({ path: path.resolve(import.meta.dirname, "..", "..", ".env"), quiet: true });
