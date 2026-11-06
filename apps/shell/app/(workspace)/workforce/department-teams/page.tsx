import { PageHeading } from "@helivanta/ui";
import { DepartmentTeamWorkspace } from "../department-team-workspace";

export default function DepartmentTeams() {
  return (
    <>
      <PageHeading description="Manage department placements, leadership and reporting responsibilities." />
      <DepartmentTeamWorkspace />
    </>
  );
}
