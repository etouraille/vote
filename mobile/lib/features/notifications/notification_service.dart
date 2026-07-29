import 'package:firebase_core/firebase_core.dart';
import 'package:firebase_messaging/firebase_messaging.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_local_notifications/flutter_local_notifications.dart';

import '../../app/config/env.dart';
import 'data/datasources/device_api.dart';

/// Android notification channel. Android groups notifications by channel
/// and lets the user mute them per channel, so one is declared explicitly
/// rather than falling back to the unnamed default.
const _androidChannel = AndroidNotificationChannel(
  'text_updates',
  'Modifications de textes',
  description: 'Notifie les textes que vous suivez lorsqu\'ils sont modifiés.',
  importance: Importance.high,
);

/// Sets up push notifications and keeps the api's copy of this device's
/// token current.
///
/// Everything here is best-effort by design: notifications are an extra,
/// and no failure in this file may keep the app from starting. Each step
/// logs and gives up rather than throwing.
class NotificationService {
  NotificationService._();

  static final _localNotifications = FlutterLocalNotificationsPlugin();

  /// Called once at startup, before runApp. Does nothing at all when the
  /// Firebase settings are absent from .env — an app built without
  /// notifications configured must still run normally.
  static Future<void> initialize() async {
    if (!Env.pushConfigured) return;

    try {
      await Firebase.initializeApp(
        // Options passed explicitly rather than read from a
        // google-services.json, so the whole configuration lives in .env
        // and the Gradle plugin isn't needed. See that file for where each
        // value comes from.
        options: FirebaseOptions(
          apiKey: Env.firebaseApiKey,
          appId: Env.firebaseAppId,
          messagingSenderId: Env.firebaseMessagingSenderId,
          projectId: Env.firebaseProjectId,
        ),
      );

      await _setUpLocalNotifications();

      // Android 13+ won't display anything without this. A refusal only
      // costs the display: the token is still registered, so the server
      // side stays consistent either way.
      await FirebaseMessaging.instance.requestPermission();

      // A message that arrives while the app is in the foreground is
      // delivered to the app instead of being shown by the system, so it
      // has to be surfaced by hand or it goes unnoticed.
      FirebaseMessaging.onMessage.listen(_showForegroundNotification);
    } catch (error) {
      debugPrint('notifications: initialisation impossible: $error');
    }
  }

  static Future<void> _setUpLocalNotifications() async {
    await _localNotifications.initialize(
      settings: const InitializationSettings(
        android: AndroidInitializationSettings('@mipmap/ic_launcher'),
      ),
    );
    await _localNotifications
        .resolvePlatformSpecificImplementation<AndroidFlutterLocalNotificationsPlugin>()
        ?.createNotificationChannel(_androidChannel);
  }

  static Future<void> _showForegroundNotification(RemoteMessage message) async {
    final notification = message.notification;
    if (notification == null) return;

    await _localNotifications.show(
      id: notification.hashCode,
      title: notification.title,
      body: notification.body,
      notificationDetails: NotificationDetails(
        android: AndroidNotificationDetails(
          _androidChannel.id,
          _androidChannel.name,
          channelDescription: _androidChannel.description,
          importance: Importance.high,
        ),
      ),
    );
  }

  /// Forgets this device server-side, so its owner stops receiving
  /// notifications once signed out.
  ///
  /// Must run while the session is still valid: the api scopes the deletion
  /// to the caller. Best-effort like everything else here — a sign-out must
  /// never be blocked by it.
  static Future<void> unregisterDevice() async {
    if (!Env.pushConfigured) return;

    try {
      final token = await FirebaseMessaging.instance.getToken();
      if (token != null) await DeviceApi.unregister(token);
    } catch (error) {
      debugPrint('notifications: désenregistrement du jeton impossible: $error');
    }
  }

  /// Registers this device against the signed-in user, and keeps doing so
  /// whenever FCM rotates the token.
  ///
  /// Must run *after* sign-in, not at startup: the api takes the owner from
  /// the bearer token, so registering without a session would be rejected.
  static Future<void> registerDevice() async {
    if (!Env.pushConfigured) return;

    try {
      final token = await FirebaseMessaging.instance.getToken();
      if (token != null) await DeviceApi.register(token);

      // FCM replaces a token on reinstall, restore, or when it expires;
      // without this the server would keep pushing to a dead one.
      FirebaseMessaging.instance.onTokenRefresh.listen((token) async {
        try {
          await DeviceApi.register(token);
        } catch (error) {
          debugPrint('notifications: enregistrement du jeton renouvelé impossible: $error');
        }
      });
    } catch (error) {
      debugPrint('notifications: enregistrement du jeton impossible: $error');
    }
  }
}
