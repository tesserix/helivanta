// Shared Go/zod emitter for contracts/workforce-*.json model definitions.
import { readFile, writeFile } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import path from "node:path";

const pascal = (value) =>
  value
    .split("_")
    .map((word) =>
      word === "id" ? "ID" : word === "ids" ? "IDs" : word[0].toUpperCase() + word.slice(1),
    )
    .join("");

export function emitModels(models, kindSchema) {
  let go = "";
  let ts = Object.values(models).some((fields) =>
    fields.some((field) => ["uuid", "[]uuid"].includes(field.type)),
  )
    ? 'const identifier=z.string().uuid("Choose a valid record.").refine(value=>value!=="00000000-0000-0000-0000-000000000000","Choose a valid record.");\n'
    : "";
  for (const [name, fields] of Object.entries(models)) {
    go += `type ${name} struct{\n`;
    ts += `export const ${name}Contract=z.object({\n`;
    for (const f of fields) {
      const array = f.type.startsWith("[]");
      const base = array ? f.type.slice(2) : f.type;
      let goType = { uuid: "uuid.UUID", time: "time.Time", kind: "string" }[base] ?? base;
      if (array) goType = "[]" + goType;
      else if (f.pointer || f.optional) goType = "*" + goType;
      const tags = [`json:"${f.name}${f.optional ? ",omitempty" : ""}"`];
      if (f.binding) tags.push(`binding:"${f.binding}"`);
      if (f.orm_ignore) tags.push('gorm:"-"');
      go += `${pascal(f.name)} ${goType} \`${tags.join(" ")}\`\n`;
      let schema =
        {
          uuid: "identifier",
          time: "z.string().datetime({ offset: true })",
          kind: kindSchema,
          int: "z.number().int()",
          int64: "z.number().int()",
          bool: "z.boolean()",
          string: "z.string()",
        }[base] ?? `${base}Contract`;
      if (f.enum) schema = `z.enum(${JSON.stringify(f.enum)})`;
      if (array) {
        schema = `z.array(${schema})`;
        if (f.max !== undefined) schema += `.max(${f.max})`;
      } else if (!f.enum) {
        if (base === "string" && ["code", "name", "text"].includes(f.format)) schema += ".trim()";
        if (f.format === "code")
          schema +=
            '.toUpperCase().regex(/^[A-Z0-9_-]{1,40}$/, "Use letters, numbers, underscores or hyphens.")';
        if (f.min !== undefined)
          schema += `.min(${f.min}${f.format === "name" ? ', "Enter a name."' : ""})`;
        if (f.max !== undefined) schema += `.max(${f.max})`;
        if (f.format === "clock")
          schema += '.regex(/^([01][0-9]|2[0-3]):[0-5][0-9]$/,"Use a 24-hour time such as 18:30.")';
        if (f.format === "date")
          schema +=
            '.date("Use a valid date.").refine(value=>value>="0001-01-01","Use a valid date.")';
        if (base === "string")
          schema += '.refine(value=>!value.includes("\\0"),"Remove invalid characters.")';
      }
      if (f.optional) schema += ".optional()";
      ts += `${f.name}:${schema},\n`;
    }
    go += "}\n\n";
    ts += `}).strict();\nexport type ${name[0].toUpperCase() + name.slice(1)}=z.infer<typeof ${name}Contract>;\n\n`;
  }
  return { go, ts };
}

// Formats both outputs, then writes them or, with --check, fails when either is stale.
export async function writeGenerated(root, goFile, go, tsFile, ts, label) {
  const outputs = {
    [goFile]: execFileSync("gofmt", [], { input: go, encoding: "utf8" }),
    [tsFile]: execFileSync("pnpm", ["exec", "prettier", "--stdin-filepath", tsFile], {
      cwd: root,
      input: ts,
      encoding: "utf8",
    }),
  };
  for (const [file, content] of Object.entries(outputs)) {
    if (process.argv.includes("--check")) {
      if ((await readFile(path.join(root, file), "utf8")) !== content)
        throw new Error(`Generated ${label} contract is stale: ${file}`);
    } else await writeFile(path.join(root, file), content);
  }
}
