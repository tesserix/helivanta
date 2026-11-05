import { readFile, writeFile } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { emitModels, writeGenerated } from "./lib/contract-codegen.mjs";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const contract = JSON.parse(
  await readFile(path.join(root, "contracts/workforce-postings.json"), "utf8"),
);
const models = emitModels(contract.models);
const validation = `
const validPostingLifecycleInput = (value:{cutoff_versions:number[];effective_until:string},ctx:z.RefinementCtx) => {
 const until=staffPostingInstantMicros(value.effective_until),lower=staffPostingInstantMicros("0001-01-01T00:00:00Z")!,upper=staffPostingInstantMicros("9999-12-31T23:59:59.999999Z")!;
 if(value.cutoff_versions[0]!==0||value.cutoff_versions.some(version=>!Number.isSafeInteger(version)||version<0||version>9007199254740990))ctx.addIssue({code:"custom",path:["cutoff_versions"],message:"Choose the exact ordered cutoff observations, including local absence."});
 if(until===null||until<=lower||until>upper)ctx.addIssue({code:"custom",path:["effective_until"],message:"Choose the exact effective lifecycle deadline."});
};
const validPostingCutoffObservation = (value:{id:string|null;version:number;cutoff_at:string|null},ctx:z.RefinementCtx) => {
 const lower=staffPostingInstantMicros("0001-01-01T00:00:00Z")!,upper=staffPostingInstantMicros("9999-12-31T23:59:59.999999Z")!;
 const instant=value.cutoff_at===null?null:staffPostingInstantMicros(value.cutoff_at);
 if(value.version===0?(value.id!==null||value.cutoff_at!==null):(value.id===null||!z.uuidv7().safeParse(value.id).success||instant===null||instant<=lower||instant>upper))ctx.addIssue({code:"custom",path:["version"],message:"The observed posting cutoff could not be verified."});
};
const validPostingCutoffInput = (value:{expected_revision_id:string;mode:string;cutoff_at?:string|null;reason:string},ctx:z.RefinementCtx) => {
 if(!z.uuidv7().safeParse(value.expected_revision_id).success)ctx.addIssue({code:"custom",path:["expected_revision_id"],message:"Choose the exact approved posting revision."});
 if(/\\p{Cc}/u.test(value.reason))ctx.addIssue({code:"custom",path:["reason"],message:"Remove control characters."});
 if(value.mode==="immediate"&&value.cutoff_at!=null)ctx.addIssue({code:"custom",path:["cutoff_at"],message:"Immediate ending uses the server instant."});
 if(value.mode==="scheduled"&&value.cutoff_at==null)ctx.addIssue({code:"custom",path:["cutoff_at"],message:"Choose an explicit future cutoff instant."});
 if(value.cutoff_at!=null){
  const instant=staffPostingInstantMicros(value.cutoff_at),lower=staffPostingInstantMicros("0001-01-01T00:00:00Z")!,upper=staffPostingInstantMicros("9999-12-31T23:59:59.999999Z")!;
  if(instant===null||instant<=lower||instant>upper)ctx.addIssue({code:"custom",path:["cutoff_at"],message:"Choose a bounded nonzero cutoff instant."});
 }
};
const validPostingCutoff = (value:{id:string;proposal_id:string;expected_revision_id:string;mode:string;requested_cutoff_at:string|null;cutoff_at:string;recorded_at:string;reason:string;expected_cutoff_version:number;version:number},ctx:z.RefinementCtx) => {
 validPostingCutoffInput({...value,cutoff_at:value.requested_cutoff_at},ctx);
 for(const key of ["id","proposal_id"] as const)if(!z.uuidv7().safeParse(value[key]).success)ctx.addIssue({code:"custom",path:[key],message:"The cutoff reference is not canonical."});
 const cutoff=staffPostingInstantMicros(value.cutoff_at),recorded=staffPostingInstantMicros(value.recorded_at),requested=value.requested_cutoff_at===null?null:staffPostingInstantMicros(value.requested_cutoff_at);
 const lower=staffPostingInstantMicros("0001-01-01T00:00:00Z")!,upper=staffPostingInstantMicros("9999-12-31T23:59:59.999999Z")!;
 if(value.version!==value.expected_cutoff_version+1||cutoff===null||recorded===null||cutoff<=lower||recorded<=lower||cutoff>upper||recorded>upper||(value.mode==="immediate"?cutoff!==recorded:cutoff!==requested||recorded>=cutoff))ctx.addIssue({code:"custom",path:["cutoff_at"],message:"The retained cutoff/version could not be verified."});
};
const validPostingCutoffContext = (value:{proposal:{id:string;revision_id:string;hospital_id:string;staff_id:string;status:string};cutoff:{proposal_id:string;expected_revision_id:string;hospital_id:string;staff_id:string;recorded_at:string}|null;checked_at:string},ctx:z.RefinementCtx) => {
 const checked=staffPostingInstantMicros(value.checked_at),lower=staffPostingInstantMicros("0001-01-01T00:00:00Z")!,upper=staffPostingInstantMicros("9999-12-31T23:59:59.999999Z")!;
 const same=(a:string,b:string)=>a.toLowerCase()===b.toLowerCase();
 const cutoff=value.cutoff,proposal=value.proposal;
 if(proposal.status!=="approved"||checked===null||checked<=lower||checked>upper||(cutoff!==null&&(!same(cutoff.proposal_id,proposal.id)||!same(cutoff.expected_revision_id,proposal.revision_id)||!same(cutoff.hospital_id,proposal.hospital_id)||!same(cutoff.staff_id,proposal.staff_id)||staffPostingInstantMicros(cutoff.recorded_at)!>checked)))ctx.addIssue({code:"custom",path:["cutoff"],message:"The scoped cutoff context could not be verified."});
};
export function staffPostingInstantMicros(value: string): bigint | null {
 const match=/^(.+T\\d{2}:\\d{2}:\\d{2})(?:\\.(\\d+))?(Z|[+-]\\d{2}:\\d{2})$/.exec(value);
 if(!match) return null;
 const millis=Date.parse(match[1]+match[3]);
 if(!Number.isFinite(millis)) return null;
 return BigInt(millis)*1000n+BigInt((match[2]??"").padEnd(6,"0").slice(0,6));
}
const validPosting = (value: {department_id:string; department_revision_id:string; starts_at:string; ends_at?:string|null; grants_access?:boolean; reason?:string}, ctx:z.RefinementCtx) => {
 for(const key of ["department_id","department_revision_id"] as const) if(!z.uuidv7().safeParse(value[key]).success) ctx.addIssue({code:"custom",path:[key],message:"Choose a current department revision."});
 const start=staffPostingInstantMicros(value.starts_at);
 const lower=staffPostingInstantMicros("0001-01-01T00:00:00Z")!;
 const upper=staffPostingInstantMicros("9999-12-31T23:59:59.999999Z")!;
 if(start===null || start<=lower || start>upper) ctx.addIssue({code:"custom",path:["starts_at"],message:"Choose a valid nonzero start instant."});
 if(value.ends_at) {
  const end=staffPostingInstantMicros(value.ends_at);
  if(end===null || start===null || end<=start || end>upper) ctx.addIssue({code:"custom",path:["ends_at"],message:"End must follow the start."});
 }
 if(value.grants_access===true) ctx.addIssue({code:"custom",path:["grants_access"],message:"Preparation cannot grant access."});
 if(value.reason && /\\p{Cc}/u.test(value.reason)) ctx.addIssue({code:"custom",path:["reason"],message:"Remove control characters."});
};

const validSupervisorChoice = (value: Parameters<typeof validPosting>[0] & {id:string;revision_id:string;staff:{id:string};eligibility_until:string;created_at:string},ctx:z.RefinementCtx) => {
 validPosting(value,ctx);
 for(const key of ["id","revision_id"] as const) if(!z.uuidv7().safeParse(value[key]).success)ctx.addIssue({code:"custom",path:[key],message:"Choose a canonical approved posting reference."});
 if(value.staff.id==="00000000-0000-0000-0000-000000000000")ctx.addIssue({code:"custom",path:["staff","id"],message:"Choose a canonical Staff identity."});
 const start=staffPostingInstantMicros(value.starts_at),until=staffPostingInstantMicros(value.eligibility_until),created=staffPostingInstantMicros(value.created_at);
 const lower=staffPostingInstantMicros("0001-01-01T00:00:00Z")!,upper=staffPostingInstantMicros("9999-12-31T23:59:59.999999Z")!;
 if(start===null||until===null||until<=start||until>upper)ctx.addIssue({code:"custom",path:["eligibility_until"],message:"The reviewed source cutoff must cover the nominal start."});
 if(created===null||created<=lower||created>upper)ctx.addIssue({code:"custom",path:["created_at"],message:"The retained intent creation time is invalid."});
};

const validProposalSubmission = (value:{draft_id:string;draft_revision_id:string;generation_id:string;reason:string},ctx:z.RefinementCtx) => {
 for(const key of ["draft_id","draft_revision_id","generation_id"] as const) if(!z.uuidv7().safeParse(value[key]).success) ctx.addIssue({code:"custom",path:[key],message:"Choose the exact reviewed source revision."});
 if(/\\p{Cc}/u.test(value.reason)) ctx.addIssue({code:"custom",path:["reason"],message:"Remove control characters."});
};
const validSupervisionIntent = (value:{kind:string;supervisor_staff_id?:string;supervisor_proposal_id?:string;supervisor_revision_id?:string},ctx:z.RefinementCtx) => {
 const staff=value.supervisor_staff_id!==undefined,proposal=value.supervisor_proposal_id!==undefined,revision=value.supervisor_revision_id!==undefined;
 if(value.kind==="hospital_management"?(staff||proposal||revision):(!staff||!proposal||!revision))ctx.addIssue({code:"custom",path:["supervisor_proposal_id"],message:"Hospital management is an explicit root; other postings require the canonical Staff member and exact supervisor proposal and revision."});
 for(const key of ["supervisor_proposal_id","supervisor_revision_id"] as const)if(value[key]!==undefined&&!z.uuidv7().safeParse(value[key]).success)ctx.addIssue({code:"custom",path:[key],message:"Choose the exact approved supervisor proposal and revision."});
};
const validProposalDecision = (value:{expected_revision_id:string;reason:string;decision:string;hospital_management_reviewed?:boolean;lifecycle_review?:unknown},ctx:z.RefinementCtx) => {
 if(value.lifecycle_review!==undefined&&value.decision!=="approve")ctx.addIssue({code:"custom",path:["lifecycle_review"],message:"Lifecycle observations apply only to approval."});
 if(!z.uuidv7().safeParse(value.expected_revision_id).success)ctx.addIssue({code:"custom",path:["expected_revision_id"],message:"Choose the exact pending proposal revision."});
 if(/\\p{Cc}/u.test(value.reason))ctx.addIssue({code:"custom",path:["reason"],message:"Remove control characters."});
 if(value.hospital_management_reviewed===true&&value.decision!=="approve")ctx.addIssue({code:"custom",path:["hospital_management_reviewed"],message:"Hospital management review applies only to approval."});
};
const validProposal = (value:{id:string;revision_id:string;command_id:string;draft_id:string;draft_revision_id:string;generation_id:string;department_id:string;department_revision_id:string;starts_at:string;ends_at:string|null;eligibility_until:string;grants_access:boolean},ctx:z.RefinementCtx) => {
 validPosting(value,ctx);
 for(const key of ["id","revision_id","command_id","draft_id","draft_revision_id","generation_id"] as const) if(!z.uuidv7().safeParse(value[key]).success) ctx.addIssue({code:"custom",path:[key],message:"The proposal source could not be verified."});
 const until=staffPostingInstantMicros(value.eligibility_until),start=staffPostingInstantMicros(value.starts_at),upper=staffPostingInstantMicros("9999-12-31T23:59:59.999999Z")!;
 if(until===null || start===null || until<=start || until>upper) ctx.addIssue({code:"custom",path:["eligibility_until"],message:"The reviewed source interval could not be verified."});
};

const validReviewSource = (value:{generation_id:string|null;generation_state:string;starts_at:string|null;ends_at:string|null;eligibility_until:string|null;checked_at:string},ctx:z.RefinementCtx) => {
 const checked=staffPostingInstantMicros(value.checked_at),start=value.starts_at===null?null:staffPostingInstantMicros(value.starts_at),end=value.ends_at===null?null:staffPostingInstantMicros(value.ends_at),until=value.eligibility_until===null?null:staffPostingInstantMicros(value.eligibility_until);
 const lower=staffPostingInstantMicros("0001-01-01T00:00:00Z")!,upper=staffPostingInstantMicros("9999-12-31T23:59:59.999999Z")!;
 const bounded=(instant:bigint|null)=>instant!==null&&instant>lower&&instant<=upper;
 let valid=bounded(checked)&&(value.generation_id===null||z.uuidv7().safeParse(value.generation_id).success);
 if(value.generation_state==="no_current_generation")valid=valid&&value.generation_id===null&&value.starts_at===null&&value.ends_at===null&&value.eligibility_until===null;
 else {
  valid=valid&&value.generation_id!==null&&bounded(start)&&(value.ends_at===null||(bounded(end)&&end!>start!));
  if(value.generation_state==="current"||value.generation_state==="not_started")valid=valid&&bounded(until)&&until!>checked!&&(value.generation_state==="current"?start!<=checked!:start!>checked!);
  if(value.generation_state==="expired")valid=valid&&bounded(until)&&until!<=checked!;
 }
 if(!valid)ctx.addIssue({code:"custom",path:["generation_state"],message:"The current reviewed Staff source could not be verified."});
};
`;
const refined = models.ts
  .replace(
    /(export const StaffPostingLifecycleReviewInputContract=z\.object\(\{[\s\S]*?\}\)\.strict\(\));/g,
    "$1.superRefine(validPostingLifecycleInput);",
  )
  .replace(
    /(export const StaffPostingCutoffObservationContract=z\.object\(\{[\s\S]*?\}\)\.strict\(\));/g,
    "$1.superRefine(validPostingCutoffObservation);",
  )
  .replace(
    /(export const StaffPostingCutoffContextContract=z\.object\(\{[\s\S]*?\}\)\.strict\(\));/g,
    "$1.superRefine(validPostingCutoffContext);",
  )
  .replace(
    /(export const StaffPostingCutoffContract=z\.object\(\{[\s\S]*?\}\)\.strict\(\));/g,
    "$1.superRefine(validPostingCutoff);",
  )
  .replace(
    /(export const StaffPostingCutoffInputContract=z\.object\(\{[\s\S]*?\}\)\.strict\(\));/g,
    "$1.superRefine(validPostingCutoffInput);",
  )
  .replace(
    /(export const StaffPostingSupervisorChoiceContract=z\.object\(\{[\s\S]*?\}\)\.strict\(\));/g,
    "$1.superRefine(validSupervisorChoice);",
  )
  .replace(
    /(export const StaffPostingSupervisionIntentContract=z\.object\(\{[\s\S]*?\}\)\.strict\(\));/g,
    "$1.superRefine(validSupervisionIntent);",
  )
  .replace(
    /(export const StaffPostingDraft(?:Input)?Contract=z\.object\(\{[\s\S]*?\}\)\.strict\(\));/g,
    "$1.superRefine(validPosting);",
  )
  .replace(
    /(export const StaffPostingProposalInputContract=z\.object\(\{[\s\S]*?\}\)\.strict\(\));/g,
    "$1.superRefine(validProposalSubmission);",
  )
  .replace(
    /(export const StaffPostingDecisionInputContract=z\.object\(\{[\s\S]*?\}\)\.strict\(\));/g,
    "$1.superRefine(validProposalDecision);",
  )
  .replace(
    /(export const StaffPostingProposalContract=z\.object\(\{[\s\S]*?\}\)\.strict\(\));/g,
    "$1.superRefine(validProposal);",
  )
  .replace(
    /(export const StaffPostingReviewSourceContract=z\.object\(\{[\s\S]*?\}\)\.strict\(\));/g,
    "$1.superRefine(validReviewSource);",
  );
await writeGenerated(
  root,
  "backend/internal/modules/workforce/posting_draft_types.go",
  '// Code generated by scripts/generate-workforce-postings.mjs; DO NOT EDIT.\npackage workforce\nimport("time";"github.com/google/uuid")\n' +
    models.go,
  "packages/api/src/staff-postings-contract.ts",
  '// Generated by scripts/generate-workforce-postings.mjs.\nimport {z} from "zod";\n' +
    validation +
    refined,
  "Staff posting drafts",
);
const events = emitModels(contract.events);
const source = execFileSync("gofmt", [], {
  input:
    '// Code generated by scripts/generate-workforce-postings.mjs; DO NOT EDIT.\npackage contract\nimport "github.com/google/uuid"\n' +
    events.go,
  encoding: "utf8",
});
const file = path.join(
  root,
  "backend/internal/modules/workforce/contract/posting_draft_generated.go",
);
if (process.argv.includes("--check")) {
  if ((await readFile(file, "utf8")) !== source)
    throw new Error("Stale Staff posting event contract");
} else await writeFile(file, source);
