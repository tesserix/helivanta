import { describe, expect, it } from "vitest";
import {
  emergencyDispatchReservationSchema,
  emergencyDispatchCommandSchema,
  emergencyClassSupports,
} from "./emergency-dispatch";

const id = "019a1234-5678-7000-8000-000000000001";
describe("dispatch commands", () => {
  it("requires explicit version-bound resources and a bounded acknowledgement window", () => {
    const body = {
      case_id: id,
      case_version: 1,
      assessment_id: id,
      vehicle_id: id,
      duty_id: id,
      acknowledgement_seconds: 120,
    };
    expect(emergencyDispatchReservationSchema.parse(body)).toEqual(body);
    for (const patch of [
      { acknowledgement_seconds: undefined },
      { acknowledgement_seconds: 14 },
      { acknowledgement_seconds: 301 },
      { case_version: 0 },
      { vehicle_id: "019a1234-5678-4000-8000-000000000001" },
      { tenant_id: id },
    ]) {
      expect(emergencyDispatchReservationSchema.safeParse({ ...body, ...patch }).success).toBe(
        false,
      );
    }
  });
  it("rejects worker-only commands and personal prose in operational references", () => {
    const body = {
      version: 2,
      operation: "acknowledge",
      evidence_reference: "CREW/DEVICE-COMMAND",
    };
    expect(emergencyDispatchCommandSchema.parse(body)).toEqual(body);
    for (const patch of [
      { operation: "offer" },
      { operation: "acknowledgement_timeout" },
      { evidence_reference: "patient Jane Doe" },
      { version: 0 },
      { actor_subject: "other" },
    ]) {
      expect(emergencyDispatchCommandSchema.safeParse({ ...body, ...patch }).success).toBe(false);
    }
  });
  it("uses the shared capability matrix without upgrading a BLS crew to ALS", () => {
    expect(emergencyClassSupports("als", "als")).toBe(true);
    expect(emergencyClassSupports("als", "bls")).toBe(true);
    expect(emergencyClassSupports("bls", "als")).toBe(false);
    expect(emergencyClassSupports("patient_transport", "bls")).toBe(false);
    expect(emergencyClassSupports(null, "patient_transport")).toBe(false);
  });
});

it("binds transport and handover commands to an explicit receiving request", () => {
  const body = {
    version: 6,
    operation: "transporting",
    evidence_reference: "SYN-TRIP",
    receiving_request_id: id,
  };
  expect(emergencyDispatchCommandSchema.parse(body)).toEqual(body);
  expect(
    emergencyDispatchCommandSchema.safeParse({ ...body, receiving_request_id: undefined }).success,
  ).toBe(false);
  expect(
    emergencyDispatchCommandSchema.safeParse({ ...body, operation: "acknowledge" }).success,
  ).toBe(false);
});
