// Time zones come from the browser: a hidden time_zone field (on the sign-up
// form) gets this device's zone, and the time zone list on the account page
// gets every zone the browser knows.
{
  const zone = Intl.DateTimeFormat().resolvedOptions().timeZone;
  for (const input of document.querySelectorAll("input[data-time-zone]")) {
    if (!input.value) input.value = zone;
  }
  const list = document.getElementById("time-zones");
  if (list && Intl.supportedValuesOf) {
    for (const name of Intl.supportedValuesOf("timeZone")) list.append(new Option(name));
  }
}
