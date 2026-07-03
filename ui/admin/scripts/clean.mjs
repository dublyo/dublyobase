import { rm } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const adminRoot = resolve(here, "..");

await rm(resolve(adminRoot, ".next"), { force: true, recursive: true });
await rm(resolve(adminRoot, "out"), { force: true, recursive: true });
