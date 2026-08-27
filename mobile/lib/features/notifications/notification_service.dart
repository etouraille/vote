import 'dart:async';
import 'dart:convert';

import 'package:firebase_core/firebase_core.dart';
import 'package:firebase_messaging/firebase_messaging.dart';
import 'package:flutter/material.dart';
import 'package:flutter_local_notifications/flutter_local_notifications.dart';

import '../../app/config/env.dart';
import '../../app/router.dart';
import '../search/presentation/pages/text_detail_page.dart';
import '../vote/presentation/pages/vote_page.dart';
import 'data/datasources/device_api.dart';
import 'notification_types.dart';
import 'unread_notifications.dart';

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

  /// What a tap asked to open before there was a Navigator to open it on.
  ///
  /// The cold-start case makes this unavoidable: the message that launched
  /// the app is read by getInitialMessage inside [initialize], which main()
  /// awaits *before* runApp — so at that moment no widget tree exists, and
  /// acting on the tap immediately is impossible rather than merely early.
  /// It is held here instead and replayed by [openPendingLaunch].
  ///
  /// Only the latest is kept: two notifications tapped before the app is up
  /// is not a real sequence, and the last one is the one the user meant.
  static ({String? type, String? textId})? _pendingLaunch;

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

      // Tapped while the app was merely backgrounded.
      FirebaseMessaging.onMessageOpenedApp.listen((message) => _openFromData(message.data));

      // Tapped while the app was not running at all: the message that
      // launched it is waiting here rather than arriving on a stream, and
      // is only ever delivered once.
      final launchMessage = await FirebaseMessaging.instance.getInitialMessage();
      if (launchMessage != null) _openFromData(launchMessage.data);
    } catch (error) {
      debugPrint('notifications: initialisation impossible: $error');
    }
  }

  static Future<void> _setUpLocalNotifications() async {
    await _localNotifications.initialize(
      settings: const InitializationSettings(
        android: AndroidInitializationSettings('@mipmap/ic_launcher'),
      ),
      // Foreground notifications are drawn by this plugin, not the system,
      // so their taps come back here rather than through
      // FirebaseMessaging.onMessageOpenedApp.
      onDidReceiveNotificationResponse: (response) => _openFromPayload(response.payload),
    );
    await _localNotifications
        .resolvePlatformSpecificImplementation<AndroidFlutterLocalNotificationsPlugin>()
        ?.createNotificationChannel(_androidChannel);
  }

  static Future<void> _showForegroundNotification(RemoteMessage message) async {
    // Bumped before the early return below: the api wrote an inbox row for
    // this event whether or not it carried anything displayable, so the
    // badge would otherwise undercount silent messages.
    UnreadNotifications.increment();

    final notification = message.notification;
    if (notification == null) return;

    await _localNotifications.show(
      id: notification.hashCode,
      title: notification.title,
      body: notification.body,
      // Carries what a tap needs through the plugin, encoded because the
      // payload is a single string: the FCM data map isn't handed back with
      // the tap, only this is. The type travels alongside the id because
      // the two together are what decide the destination.
      payload: jsonEncode({
        'type': message.data['type'],
        'textId': message.data['textId'],
      }),
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

  /// Opens whatever a tapped notification pointed at.
  ///
  /// Both the id and the kind travel in the message's data map (see the
  /// api's EditProposed), not in its title or body: the visible text is for
  /// the reader, the data is what the app acts on.
  static void _openFromData(Map<String, dynamic> data) {
    _open(data['type'] as String?, data['textId'] as String?);
  }

  /// The same, for a notification drawn by the local plugin rather than by
  /// Android — its tap hands back only the payload string set above.
  static void _openFromPayload(String? payload) {
    if (payload == null || payload.isEmpty) return;

    try {
      final data = jsonDecode(payload) as Map<String, dynamic>;
      _open(data['type'] as String?, data['textId'] as String?);
    } catch (_) {
      // A payload written before this carried anything but the bare id.
      // Treating it as one keeps a notification still sitting in the
      // drawer across an app update from becoming a dead tap.
      _open(null, payload);
    }
  }

  /// Routes by kind, the same way the in-app inbox does (see
  /// NotificationsPage) — a notification has to lead to the same place
  /// whichever of the two it is tapped from.
  static void _open(String? type, String? textId) {
    if (textId == null || textId.isEmpty) return;

    // Navigating from outside the widget tree — a tap handler has no
    // BuildContext of its own. Null while the app is still starting up,
    // where the destination is remembered rather than dropped: this is the
    // normal path for a notification that launched the app.
    final navigator = AppRouter.navigatorKey.currentState;
    if (navigator == null) {
      _pendingLaunch = (type: type, textId: textId);
      return;
    }

    navigator.push(MaterialPageRoute(
      builder: (_) => NotificationTypes.opensVote(type)
          ? VotePage(textId: textId)
          : TextDetailPage(textId: textId),
    ));
  }

  /// Opens whatever a tap asked for while the app had no Navigator yet.
  ///
  /// Called once the first frame is built, and again after a sign-in: the
  /// pages a notification leads to all need a session, so a tap that
  /// launched a signed-out app is honoured at the end of the login it
  /// prompted rather than thrown away.
  ///
  /// Consumes what it replays, so a later call is a no-op instead of
  /// reopening the same text.
  static void openPendingLaunch() {
    final pending = _pendingLaunch;
    if (pending == null) return;

    _pendingLaunch = null;
    _open(pending.type, pending.textId);
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

      // Signing in on a device that was away: whatever arrived meanwhile
      // is already in the inbox, and the badge has to say so.
      unawaited(UnreadNotifications.refresh());

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
