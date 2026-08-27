// Service worker for Firebase Web Push.
//
// It has to sit at the root of the origin — a service worker can only
// control pages at or below its own path, and firebase-messaging looks for
// it at exactly /firebase-messaging-sw.js. Angular copies public/ to the
// build root, which is what puts it there.
//
// Loaded from the CDN with importScripts rather than bundled: a service
// worker is a separate entry point that the Angular build does not compile,
// and the compat build is the one designed to be used this way.
importScripts('https://www.gstatic.com/firebasejs/10.14.1/firebase-app-compat.js');
importScripts('https://www.gstatic.com/firebasejs/10.14.1/firebase-messaging-compat.js');

// Duplicated from src/app/firebase-config.ts, and unavoidably so: this file
// is not part of the Angular build and cannot import from it. Keep the two
// in step — a mismatch makes getToken() hand back a token for a different
// app, which the api then pushes to and never reaches anyone.
firebase.initializeApp({
  apiKey: 'AIzaSyCvTrNDe4vAxbUFbWMzJGR4zQaeSV9s6QY',
  messagingSenderId: '1082472974181',
  projectId: 'queel-9dc33',
  appId: 'AIzaSyBO340xEnhzK6h0inUCWky1n8enzqEsviQ',
});

// Nothing else to do: a message carrying a `notification` block is drawn by
// the browser on its own while the tab is closed. This handler exists for
// data-only messages, which the browser would otherwise drop silently.
//
// The api always sends a notification block (see notify.PushChannel), so in
// practice this is the safety net rather than the normal path.
firebase.messaging().onBackgroundMessage((payload) => {
  if (payload.notification) return;

  const data = payload.data || {};
  self.registration.showNotification(data.title || 'Queel', {
    body: data.body || '',
    // Carries the event through the click below, the same way the mobile
    // app threads it through its local-notification payload.
    data,
  });
});

// A tapped notification focuses an already-open tab rather than opening a
// second one, and asks it to navigate — the page knows how to route by
// event type, this worker does not.
self.addEventListener('notificationclick', (event) => {
  event.notification.close();

  const data = event.notification.data || {};
  event.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((clients) => {
      for (const client of clients) {
        if ('focus' in client) {
          client.postMessage({ type: 'queel:notification-click', data });
          return client.focus();
        }
      }
      return self.clients.openWindow('/notifications');
    }),
  );
});
