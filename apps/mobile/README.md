# Helivanta mobile apps

The citizen AmbulanceConnect app is implemented in
[`apps/ambulanceconnect`](../ambulanceconnect/README.md). Its current call-first
slice opens the India emergency dialler without login or data connectivity;
ambulance booking and live tracking remain programme #1220 work.

Doctor, patient, nurse and pharmacist apps remain reserved per spec D7 (issues
#11, #566, #567). Crew workflows #124/#132 require their own authenticated,
encrypted offline experience; public citizen access must not acquire crew or
clinical-record permissions. New clients use Expo and shared contracts.
