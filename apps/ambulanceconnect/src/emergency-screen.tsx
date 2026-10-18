import { useRef, useState } from "react";
import { Linking, Pressable, ScrollView, Text, View } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import {
  emergencyAccess,
  emergencyCopy as copy,
  type EmergencyCountry,
} from "@helivanta/emergency-access";

export function EmergencyScreen() {
  const [country, setCountry] = useState<EmergencyCountry>("IN");
  const [dialError, setDialError] = useState(false);
  const [opening, setOpening] = useState(false);
  const attempt = useRef(0);
  const inFlight = useRef(false);
  const access = emergencyAccess(country);
  const service = access.services[0];

  function chooseCountry(next: EmergencyCountry) {
    attempt.current += 1;
    inFlight.current = false;
    setOpening(false);
    setDialError(false);
    setCountry(next);
  }

  async function openDialler() {
    if (!service || inFlight.current) return;
    inFlight.current = true;
    const currentAttempt = ++attempt.current;
    setOpening(true);
    setDialError(false);
    try {
      await Linking.openURL(service.dialUrl);
    } catch {
      if (currentAttempt === attempt.current) setDialError(true);
    } finally {
      if (currentAttempt === attempt.current) {
        inFlight.current = false;
        setOpening(false);
      }
    }
  }

  return (
    <SafeAreaView className="flex-1 bg-slate-50">
      <ScrollView contentContainerStyle={{ flexGrow: 1 }}>
        <View className="mx-auto w-full max-w-2xl gap-6 px-5 py-6">
          <View className="gap-2">
            <Text className="text-lg font-semibold text-teal-800">{copy.brand}</Text>
            <Text className="text-sm text-slate-600">{copy.byline}</Text>
          </View>
          <Text accessibilityRole="header" className="text-4xl font-semibold text-slate-950">
            {copy.title}
          </Text>
          <Text className="text-lg leading-7 text-slate-700">{copy.introduction}</Text>
          <View className="gap-3">
            <Text className="font-semibold text-slate-900">{copy.countryLabel}</Text>
            <View className="flex-row flex-wrap gap-3">
              {(["IN", "OTHER"] as const).map((code) => (
                <Pressable
                  key={code}
                  accessibilityRole="button"
                  accessibilityState={{ selected: country === code }}
                  onPress={() => chooseCountry(code)}
                  className={
                    country === code
                      ? "min-h-12 justify-center rounded-xl border-2 border-teal-700 bg-teal-50 px-4 py-3"
                      : "min-h-12 justify-center rounded-xl border border-slate-300 bg-white px-4 py-3"
                  }
                  style={({ pressed }) => ({ opacity: pressed ? 0.7 : 1 })}
                >
                  <Text className="text-base font-semibold text-slate-900">
                    {code === "IN" ? copy.india : copy.otherCountry}
                  </Text>
                </Pressable>
              ))}
            </View>
          </View>
          {service ? (
            <View className="gap-4 rounded-3xl border border-slate-200 bg-white p-5">
              <Text accessibilityRole="header" className="text-xl font-semibold text-slate-900">
                {service.title}
              </Text>
              <Text className="text-6xl font-semibold text-teal-800" selectable>
                {service.number}
              </Text>
              <Pressable
                accessibilityRole="button"
                accessibilityLabel={copy.callLabel}
                accessibilityState={{ disabled: opening, busy: opening }}
                disabled={opening}
                onPress={openDialler}
                className="min-h-14 items-center justify-center rounded-2xl bg-teal-800 px-5 py-4"
                style={({ pressed }) => ({ opacity: pressed || opening ? 0.7 : 1 })}
              >
                <Text className="text-center text-lg font-semibold text-white">
                  {copy.callLabel}
                </Text>
              </Pressable>
              <Text className="text-base leading-6 text-slate-700">{copy.callNotice}</Text>
              {dialError ? (
                <Text
                  accessibilityRole="alert"
                  accessibilityLiveRegion="assertive"
                  className="text-base font-semibold text-red-800"
                >
                  {copy.callFailure}
                </Text>
              ) : null}
            </View>
          ) : (
            <Text
              accessibilityLiveRegion="polite"
              className="rounded-2xl bg-amber-50 p-5 text-lg leading-7 text-amber-950"
            >
              {copy.unsupported}
            </Text>
          )}
          {access.regionalInformation.length > 0 ? (
            <View className="gap-4 rounded-3xl border border-slate-200 p-5">
              <Text accessibilityRole="header" className="text-lg font-semibold text-slate-900">
                {copy.regionalTitle}
              </Text>
              {access.regionalInformation.map((regional) => (
                <View key={regional.number} className="gap-1">
                  <Text className="text-2xl font-semibold text-slate-900">{regional.number}</Text>
                  <Text className="text-base leading-6 text-slate-700">{regional.scope}</Text>
                </View>
              ))}
              <Text className="text-sm leading-6 text-slate-600">{copy.regionalNotice}</Text>
            </View>
          ) : null}
          <View className="gap-3 rounded-3xl bg-teal-50 p-5">
            <Text className="text-base font-semibold text-teal-950">{copy.bookingNotice}</Text>
            <Text className="text-sm leading-6 text-teal-950">{copy.privacyNotice}</Text>
          </View>
          <View className="gap-2 border-t border-slate-200 pt-5">
            <Text className="text-sm font-semibold text-slate-900">{copy.evidenceTitle}</Text>
            <Text className="text-sm text-slate-600">
              {copy.reviewedLabel}: {access.reviewedOn}
            </Text>
            <Text className="text-sm text-slate-600">
              {copy.revisionLabel}: {access.revision}
            </Text>
            {service ? (
              <Text className="text-xs leading-5 text-slate-600" selectable>
                {service.sourceUrl}
              </Text>
            ) : null}
          </View>
        </View>
      </ScrollView>
    </SafeAreaView>
  );
}
