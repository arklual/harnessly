// The weather of the live effect «Погода» (fx/weather.js). The effect's frame has no network, so the page asks for it:
// the place from the browser's geolocation (the browser asks the person once; rounded to 0.1°, about 10 km, before it
// leaves the page), the weather from Open-Meteo (no key, no account), every 15 minutes, one request for every frame
// that listens. Without a place — refused, no geolocation, no answer — the effect still follows the clock: the sun by
// the time of day and the season, a clear sky.

export interface IWeather {
  /** Where the weather is from: open-meteo, or the clock alone. */
  source: 'open-meteo' | 'clock';
  lat: number | null;
  lon: number | null;
  /** WMO weather code (0 clear … 99 thunderstorm with hail). */
  code: number;
  /** Cloud cover, %. */
  cloud: number;
  /** °C. */
  temp: number;
  /** Wind at 10 m, m/s, and where it blows from, degrees. */
  wind: number;
  windDir: number;
  /** mm (rain) and cm (snow) in the last hour. */
  rain: number;
  snowfall: number;
  /** Today's sunrise and sunset, ms since the epoch; null without a place. */
  sunrise: number | null;
  sunset: number | null;
}

const EVERY = 15 * 60_000;
const CLOCK: IWeather = {
  source: 'clock',
  lat: null,
  lon: null,
  code: 0,
  cloud: 15,
  temp: 0,
  wind: 3,
  windDir: 270,
  rain: 0,
  snowfall: 0,
  sunrise: null,
  sunset: null,
};

let last: IWeather | null = null;
let timer: ReturnType<typeof setInterval> | null = null;
const listeners = new Set<(w: IWeather) => void>();

const tell = (w: IWeather) => {
  last = w;
  listeners.forEach((fn) => fn(w));
};

const num = (v: unknown, d: number): number => (typeof v === 'number' && Number.isFinite(v) ? v : d);

/** A local time of Open-Meteo («2026-11-16T08:12») at a UTC offset, as ms since the epoch. */
const localMs = (s: unknown, offset: number): number | null =>
  typeof s === 'string' && !Number.isNaN(Date.parse(`${s}:00Z`))
    ? Date.parse(`${s}:00Z`) - offset * 1000
    : null;

const place = (): Promise<{ lat: number; lon: number } | null> =>
  new Promise((done) => {
    if (typeof navigator === 'undefined' || !navigator.geolocation) return done(null);
    navigator.geolocation.getCurrentPosition(
      (p) =>
        done({ lat: Math.round(p.coords.latitude * 10) / 10, lon: Math.round(p.coords.longitude * 10) / 10 }),
      () => done(null),
      { maximumAge: 6 * 3600_000, timeout: 20_000, enableHighAccuracy: false },
    );
  });

const fetchWeather = async (): Promise<IWeather> => {
  const at = await place();
  if (!at) return CLOCK;
  const q = new URLSearchParams({
    latitude: String(at.lat),
    longitude: String(at.lon),
    current: 'temperature_2m,weather_code,cloud_cover,wind_speed_10m,wind_direction_10m,rain,snowfall',
    daily: 'sunrise,sunset',
    timezone: 'auto',
    forecast_days: '1',
    wind_speed_unit: 'ms',
  });
  try {
    const res = await fetch(`https://api.open-meteo.com/v1/forecast?${q}`, { credentials: 'omit' });
    if (!res.ok) return { ...CLOCK, lat: at.lat, lon: at.lon };
    const j = (await res.json()) as {
      utc_offset_seconds?: number;
      current?: Record<string, unknown>;
      daily?: { sunrise?: unknown[]; sunset?: unknown[] };
    };
    const c = j.current ?? {};
    const off = num(j.utc_offset_seconds, 0);
    return {
      source: 'open-meteo',
      lat: at.lat,
      lon: at.lon,
      code: num(c.weather_code, 0),
      cloud: num(c.cloud_cover, 0),
      temp: num(c.temperature_2m, 0),
      wind: num(c.wind_speed_10m, 0),
      windDir: num(c.wind_direction_10m, 270),
      rain: num(c.rain, 0),
      snowfall: num(c.snowfall, 0),
      sunrise: localMs(j.daily?.sunrise?.[0], off),
      sunset: localMs(j.daily?.sunset?.[0], off),
    };
  } catch {
    return { ...CLOCK, lat: at.lat, lon: at.lon };
  }
};

const refresh = () => void fetchWeather().then(tell);

/** Calls fn with the weather now (at once when known) and on each change; returns the unsubscribe. */
export const watchWeather = (fn: (w: IWeather) => void): (() => void) => {
  listeners.add(fn);
  if (last) fn(last);
  if (!timer) {
    refresh();
    timer = setInterval(refresh, EVERY);
  }
  return () => {
    listeners.delete(fn);
    if (listeners.size === 0 && timer) {
      clearInterval(timer);
      timer = null;
    }
  };
};

/** The last weather told, for a frame that became ready after it came. */
export const lastWeather = (): IWeather | null => last;
