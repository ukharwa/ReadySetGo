// Time zones come from the browser: a hidden time_zone field (on the sign-up
// form) gets this device's zone, and the time zone list on the account page
// gets every zone the browser knows.
{
  const zone = Intl.DateTimeFormat().resolvedOptions().timeZone;
  for (const input of document.querySelectorAll("input[data-time-zone]")) {
    if (!input.value) input.value = zone;
  }
  // readysetgo:begin module=social
  // Links that sign up with another service pass the zone on too.
  for (const link of document.querySelectorAll("a[data-time-zone]")) {
    const url = new URL(link.href);
    url.searchParams.set("time_zone", zone);
    link.href = url;
  }
  // readysetgo:end
  const list = document.getElementById("time-zones");
  if (list && Intl.supportedValuesOf) {
    for (const name of Intl.supportedValuesOf("timeZone")) list.append(new Option(name));
  }
}
