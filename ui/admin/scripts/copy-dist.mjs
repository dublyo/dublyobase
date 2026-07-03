import { cp, rm } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const adminRoot = resolve(here, "..");
const uiRoot = resolve(adminRoot, "..");

await rm(resolve(uiRoot, "dist"), { force: true, recursive: true });
await cp(resolve(adminRoot, "out"), resolve(uiRoot, "dist"), { recursive: true });
