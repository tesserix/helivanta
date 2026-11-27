import { readFile, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import path from "node:path";
import { emitModels, writeGenerated } from "./lib/contract-codegen.mjs";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const source = JSON.parse(
  await readFile(path.join(root, "contracts/pharmacy-verification.json"), "utf8"),
);
const decisions = source.models.PrescriptionVerificationDecisionInput.find(
  (field) => field.name === "decision",
).enum;
if (
  Object.keys(source.decisions).length !== decisions.length ||
  decisions.some((decision) => {
    const rule = source.decisions[decision];
    return (
      !rule ||
      typeof rule.requires_reason !== "boolean" ||
      typeof rule.requires_amendment !== "boolean"
    );
  })
) {
  throw new Error("Every verification decision needs explicit intent requirements");
}
const modelFields = Object.fromEntries(
  Object.entries(source.models).map(([name, fields]) => [
    name,
    source.precondition_models.includes(name) ? [...source.precondition_fields, ...fields] : fields,
  ]),
);
const models = emitModels(modelFields);
const fields = Object.fromEntries(
  modelFields.PrescriptionVerificationDecisionInput.map((field) => [field.name, field]),
);
const policyFields = Object.fromEntries(
  source.models.VerificationPolicyDraftInput.map((field) => [field.name, field]),
);
const rules = `
const verificationPolicyMaxVersion=${policyFields.expected_version.max}
const verificationPolicyLabelMax=${policyFields.label.max}
const verificationPolicyReasonMax=${policyFields.reason.max}
const verificationPolicyReviewDurationMax=${policyFields.review_valid_for_seconds.max}
const verificationPolicyResponseDurationMax=${policyFields.query_response_target_seconds.max}
const verificationPolicyLabelPattern=${JSON.stringify(policyFields.label.pattern)}
const verificationMaxVersion=${fields.expected_version.max}
const verificationNoteMaxUTF16=${fields.clinical_note.max}
const verificationReasonMax=${fields.reason_code.max}
const verificationHashPattern=${JSON.stringify(fields.reviewed_state_hash.pattern)}
const verificationReaderPattern=${JSON.stringify(source.models.PrescriptionReviewContext.find((field) => field.name === "reader_subject").pattern)}
const verificationReasonPattern=${JSON.stringify(fields.reason_code.pattern)}
const verificationBlankNoteCharacters=${JSON.stringify(source.blank_note_characters).replace(/[^\x20-\x7e]/gu, (char) => "\\u" + char.charCodeAt(0).toString(16).padStart(4, "0"))}
func verificationDecisionRequirements(decision string)(reason,amendment,known bool){
switch decision{
${decisions.map((decision) => `case ${JSON.stringify(decision)}: return ${source.decisions[decision].requires_reason},${source.decisions[decision].requires_amendment},true`).join("\n")}
default:return false,false,false
}}
`;
const decisionsTS = models.ts.replace(
  /(export const PrescriptionVerificationDecisionInputContract=z.object\(\{[\s\S]*?\}\)\.strict\(\))/,
  `$1.superRefine((value,ctx)=>{
    const rule=verificationDecisionRequirements[value.decision];
    if(value.clinical_note!==null && value.clinical_note.length>verificationNoteMaxUTF16)
      ctx.addIssue({code:"custom",path:["clinical_note"],message:"The clinical note exceeds the permitted length."});
    if(value.reason_code!==null && verificationReasonPattern.exec(value.reason_code)?.[0]!==value.reason_code)
      ctx.addIssue({code:"custom",path:["reason_code"],message:"Choose a valid decision reason code."});
    if(rule.requires_reason && value.reason_code===null)
      ctx.addIssue({code:"custom",path:["reason_code"],message:"Choose a reason for this decision."});
    if(rule.requires_reason && (value.clinical_note===null || [...value.clinical_note].every(char=>verificationBlankNoteCharacters.includes(char))))
      ctx.addIssue({code:"custom",path:["clinical_note"],message:"Record the clinical reason for this decision."});
    if(rule.requires_amendment !== (value.prescriber_amendment_id!==null))
      ctx.addIssue({code:"custom",path:["prescriber_amendment_id"],message:"Reference the prescriber's amendment only when approving a changed prescription."});
  })`,
);
if (decisionsTS === models.ts) {
  throw new Error("Verification decision requirements were not applied to the generated schema");
}
const ts = decisionsTS
  .replace(
    /(export const PrescriptionReviewLineContract=z.object\(\{[\s\S]*?\}\)\.strict\(\))/,
    `$1.superRefine((value,ctx)=>{
      if(value.units_remaining>value.quantity_units)ctx.addIssue({code:"custom",path:["units_remaining"],message:"The remaining quantity exceeds the recorded prescription."});
      if(value.dispensings_remaining>value.dispensings_allowed)ctx.addIssue({code:"custom",path:["dispensings_remaining"],message:"The repeat balance exceeds the recorded prescription."});
    })`,
  )
  .replace(
    /(export const PrescriptionReviewRecordContract=z.object\(\{[\s\S]*?\}\)\.strict\(\))/,
    `$1.superRefine((value,ctx)=>{
      if(value.lines.some((line,index)=>line.line_number!==index+1))ctx.addIssue({code:"custom",path:["lines"],message:"The recorded prescription lines are incomplete or out of order."});
    })`,
  );
if (ts === decisionsTS) {
  throw new Error("Prescription source consistency checks were not applied");
}
await writeGenerated(
  root,
  "backend/internal/modules/pharmacy/verification_inputs_generated.go",
  '// Code generated by scripts/generate-pharmacy-verification.mjs; DO NOT EDIT.\npackage pharmacy\nimport("time";"github.com/google/uuid")\n' +
    models.go +
    rules,
  "packages/api/src/pharmacy-verification-contract.ts",
  '// Generated by scripts/generate-pharmacy-verification.mjs. Decision inputs remain draft; review context grants no clinical authority.\nimport {z} from "zod";\n' +
    `const verificationDecisionRequirements=${JSON.stringify(source.decisions)} as const;\n` +
    `const verificationNoteMaxUTF16=${fields.clinical_note.max};\n` +
    `const verificationReasonPattern=new RegExp(${JSON.stringify(fields.reason_code.pattern)},"u");\n` +
    `const verificationBlankNoteCharacters=${JSON.stringify(source.blank_note_characters)};\n` +
    ts,
  "pharmacy verification",
);

const blankSQL = [...source.blank_note_characters]
  .map((char) => "\\" + char.codePointAt(0).toString(16).padStart(4, "0"))
  .join("");
const labelSQL = policyFields.label.pattern.replace("\\x00", "\\x01");
const policySQL = `-- Generated by scripts/generate-pharmacy-verification.mjs; DO NOT EDIT.
CREATE FUNCTION pharmacy_verification_utf16_length(value text) RETURNS integer
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$
 SELECT char_length(value)+char_length(regexp_replace(value,U&'[^\\+010000-\\+10FFFF]','','g'))
$$;
CREATE FUNCTION pharmacy_verification_policy_draft_contract(
 expected bigint,label text,second_check boolean,review_seconds bigint,response_seconds bigint,reason text
) RETURNS boolean LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$
 SELECT expected BETWEEN 0 AND ${policyFields.expected_version.max}
 AND label=btrim(label,U&'${blankSQL}') AND label ~ '${labelSQL}'
 AND pharmacy_verification_utf16_length(label) BETWEEN 1 AND ${policyFields.label.max}
 AND reason=btrim(reason,U&'${blankSQL}')
 AND pharmacy_verification_utf16_length(reason) BETWEEN 1 AND ${policyFields.reason.max}
 AND review_seconds BETWEEN 1 AND ${policyFields.review_valid_for_seconds.max}
 AND response_seconds BETWEEN 1 AND ${policyFields.query_response_target_seconds.max}
 AND second_check IS NOT NULL
$$;
`;
const policySQLFile =
  "backend/internal/modules/pharmacy/verification_policy_contract_generated.sql";
if (process.argv.includes("--check")) {
  if ((await readFile(path.join(root, policySQLFile), "utf8")) !== policySQL)
    throw new Error("Generated policy SQL contract is stale");
} else await writeFile(path.join(root, policySQLFile), policySQL);
