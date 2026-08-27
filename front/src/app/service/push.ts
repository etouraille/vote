import { Service, inject } from '@angular/core';
import { deleteToken, getMessaging, getToken, onMessage, Messaging } from 'firebase/messaging';
import { initializeApp, FirebaseApp } from 'firebase/app';
import { HttpClient } from '@angular/common/http';
import { firstValueFrom } from 'rxjs';
import { API_BASE_URL } from '../api-base-url';
import { FIREBASE_CONFIG, FIREBASE_VAPID_KEY, pushConfigured } from '../firebase-config';
import { NotificationService } from './notification';

// Sets up Firebase Web Push for the browser and keeps the api's copy of
// this browser's token current — the web counterpart of the mobile app's
// NotificationService.
//
// Everything here is best-effort by design. Notifications are an extra, and
// the inbox already carries every event server-side, so no failure in this
// file may disturb the page: each step logs and gives up.
@Service()
export class PushService {
  private readonly http = inject(HttpClient);
  private readonly notifications = inject(NotificationService);

  private app: FirebaseApp | null = null;
  private messaging: Messaging | null = null;
  private token: string | null = null;

  // Registers this browser against the signed-in user.
  //
  // Must run *after* sign-in, never at startup: the api takes the device's
  // owner from the bearer token, so registering without a session is
  // refused — the same rule the mobile app follows.
  async register(): Promise<void> {
    if (!pushConfigured() || !this.supported()) return;

    try {
      const messaging = this.init();
      if (!messaging) return;

      // Asked here rather than on page load: a permission prompt that
      // appears before anyone has done anything is the one people refuse
      // out of hand, and a refusal is remembered by the browser.
      if ((await Notification.requestPermission()) !== 'granted') return;

      // registration is passed explicitly so the SDK uses our own service
      // worker rather than registering a second one of its own.
      const registration = await navigator.serviceWorker.register('/firebase-messaging-sw.js');
      const token = await getToken(messaging, {
        vapidKey: FIREBASE_VAPID_KEY,
        serviceWorkerRegistration: registration,
      });
      if (!token) return;

      this.token = token;
      await firstValueFrom(
        this.http.post(`${API_BASE_URL}/api/me/devices`, { token, platform: 'web' }),
      );

      // A message arriving while the tab has focus is delivered here
      // instead of being drawn by the browser, so it would go unnoticed —
      // the same trap the mobile app hits in the foreground. Refreshing
      // the badge is what makes it visible without a reload.
      onMessage(messaging, () => this.notifications.refreshUnread());
    } catch (error) {
      console.debug('push: registration failed', error);
    }
  }

  // Forgets this browser server-side, so its owner stops being pushed to
  // once signed out.
  //
  // Must run while the session is still valid: the api scopes the deletion
  // to the caller, so it has nothing to match on once the token is gone.
  async unregister(): Promise<void> {
    if (!this.token) return;
    const token = this.token;
    this.token = null;

    try {
      await firstValueFrom(
        this.http.request('delete', `${API_BASE_URL}/api/me/devices`, { body: { token } }),
      );
      if (this.messaging) await deleteToken(this.messaging);
    } catch (error) {
      console.debug('push: unregistration failed', error);
    }
  }

  private init(): Messaging | null {
    if (this.messaging) return this.messaging;

    this.app = initializeApp(FIREBASE_CONFIG);
    this.messaging = getMessaging(this.app);
    return this.messaging;
  }

  // Web Push needs a service worker and the Push API, and browsers only
  // expose them over HTTPS — localhost excepted, which is why development
  // works and a plain-http deployment would not.
  private supported(): boolean {
    return (
      typeof Notification !== 'undefined' && 'serviceWorker' in navigator && 'PushManager' in window
    );
  }
}
