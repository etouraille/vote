// Firebase Web Push configuration.
//
// The project is the one the mobile app already pushes through
// (queel-9dc33, see mobile/lib/config/.env), so apiKey, messagingSenderId
// and projectId are its values verbatim — a web client and an Android
// client of the same project share them.
//
// appId does not: it identifies one *app* within the project, so the web
// front needs its own, from Firebase console > Project settings > Your apps
// > Add app > Web.
//
// vapidKey is the public half of the key pair browsers require to accept a
// push subscription, from Project settings > Cloud Messaging > Web
// configuration > Web Push certificates. getToken() cannot work without it.
//
// Both are public by design — they identify the app, they don't authorize
// anything. What authorizes sending is the service-account key the api
// holds (api/firebase-key.json), which never reaches a browser.
export const FIREBASE_CONFIG = {
  apiKey: 'AIzaSyBO340xEnhzK6h0inUCWky1n8enzqEsviQ',
  messagingSenderId: '1082472974181',
  projectId: 'queel-9dc33',
  appId: '1:1082472974181:web:225cd2d16e96536445438c',
};

export const FIREBASE_VAPID_KEY = 'BGdHt_ntnZSaR8M3AMwLH5vBl_MXmgqEBj-bLIXj1gMNVFK8Z474uPe1_KDOc18lMgfzk-LGdihyM78PPA2kX9E';

// Whether push can be set up at all. Deliberately a silent no rather than a
// throw, the same choice the mobile app makes (see Env.pushConfigured): a
// front built without push configured must still run, it just stays quiet
// and leaves the inbox as the only way notifications arrive.
export function pushConfigured(): boolean {
  return FIREBASE_CONFIG.appId !== '' && FIREBASE_VAPID_KEY !== 'BGdHt_ntnZSaR8M3AMwLH5vBl_MXmgqEBj-bLIXj1gMNVFK8Z474uPe1_KDOc18lMgfzk-LGdihyM78PPA2kX9E';
}
