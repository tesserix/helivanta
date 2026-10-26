import { expect, it } from "vitest";
import { moduleStatus } from "@/app/platform-catalog";
import { stations } from "./patient-journey-data";

it("walks one patient through eight stations in the order a hospital visit happens", () => {
  expect(stations.map((station) => station.id)).toEqual([
    "registration",
    "consultation",
    "lab-order",
    "result",
    "prescription",
    "dispensing",
    "billing",
    "discharge",
  ]);
});

it("names the team that owns every record entry", () => {
  for (const station of stations) {
    expect(station.owner, station.id).toMatch(/\S/);
    expect(station.record, station.id).toMatch(/\S/);
  }
});

it("resolves every station to its catalogue status", () => {
  for (const station of stations) expect(moduleStatus(station.module)).toMatch(/\S/);
});
