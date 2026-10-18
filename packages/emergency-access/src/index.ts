export type EmergencyCountry = "IN" | "OTHER";

export const emergencyCopy = {
  brand: "AmbulanceConnect",
  byline: "Emergency access by Helivanta",
  title: "Need emergency help?",
  introduction:
    "If you are in India, call 112 for emergency help. You do not need a Helivanta account.",
  countryLabel: "Where are you?",
  india: "India",
  otherCountry: "Outside India",
  unsupported:
    "Outside India, contact the local emergency service. This directory does not yet cover your country.",
  nationalTitle: "India emergency assistance",
  callLabel: "Open dialler for 112",
  callNotice:
    "This opens your phone dialler. It does not book an ambulance or confirm that help is on the way. Explain your location and situation to the emergency operator.",
  callFailure: "The dialler could not open. Use a phone to dial manually: 112.",
  regionalTitle: "About other ambulance numbers",
  regionalNotice:
    "108 and 102 service coverage varies by state. Confirm local operation; these listings are not live availability. For emergency help in India, call 112.",
  emergency108: "Emergency response in participating states",
  transport102: "Patient transport, particularly for pregnant women and children",
  bookingNotice: "Ambulance booking and live tracking are not available here yet.",
  privacyNotice: "This page does not request your location, contact details or health information.",
  evidenceTitle: "Published service information",
  evidenceLabel: "National Ambulance Services guidelines (2026)",
  reviewedLabel: "Information reviewed",
  revisionLabel: "Directory version",
  home: "Helivanta home",
} as const;

const nationalSource =
  "https://nhsrcindia.org/sites/default/files/2026-09/Operational%20Guidelines%20for%20National%20Ambulance%20Services.pdf";
const regionalSource = "https://nhm.gov.in/index1.php?lang=1&level=2&sublinkid=1217&lid=189";

type EmergencyService = {
  kind: "national_emergency";
  number: string;
  dialUrl: `tel:${string}`;
  title: string;
  sourceUrl: string;
};

type RegionalInformation = { number: string; scope: string; sourceUrl: string };

export type EmergencyAccess = {
  revision: string;
  reviewedOn: string;
  countryCode: EmergencyCountry;
  countryName: string;
  bookingEnabled: false;
  services: EmergencyService[];
  regionalInformation: RegionalInformation[];
};

export function emergencyAccess(country: string): EmergencyAccess {
  const india = country === "IN";
  return {
    revision: "2026-10-18.1",
    reviewedOn: "2026-10-18",
    countryCode: india ? "IN" : "OTHER",
    countryName: india ? emergencyCopy.india : emergencyCopy.otherCountry,
    bookingEnabled: false,
    services: india
      ? [
          {
            kind: "national_emergency",
            number: "112",
            dialUrl: "tel:112",
            title: emergencyCopy.nationalTitle,
            sourceUrl: nationalSource,
          },
        ]
      : [],
    regionalInformation: india
      ? [
          { number: "108", scope: emergencyCopy.emergency108, sourceUrl: regionalSource },
          { number: "102", scope: emergencyCopy.transport102, sourceUrl: regionalSource },
        ]
      : [],
  };
}
