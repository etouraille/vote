import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_native_splash/flutter_native_splash.dart';

import 'app/app.dart';
import 'app/config/env.dart';
import 'core/storage/secure_storage.dart';
import 'features/notifications/notification_service.dart';
import 'features/authentication/presentation/pages/login_page.dart';
import 'features/search/presentation/pages/search_page.dart';

Future<void> main() async {
  final widgetsBinding = WidgetsFlutterBinding.ensureInitialized();
  // Keeps the native splash (see pubspec.yaml's flutter_native_splash config
  // — the same queel_splash.png the OS already shows before Flutter is even
  // up) on screen past Flutter's first frame, instead of it being
  // auto-dismissed and replaced by a second, differently-sized logo drawn
  // by our own UI.
  FlutterNativeSplash.preserve(widgetsBinding: widgetsBinding);

  // Env.load() finishes near-instantly, which would otherwise let the
  // splash vanish after a single frame — wait for both it and a 2s minimum
  // so it's actually visible, without adding to the wait on a slower load.
  // Done before runApp so the very first screen already knows where to
  // land, since it never re-decides that on its own afterward.
  final envLoaded = Env.load();
  final tokenRead = SecureStorage.readValidToken();
  final minSplashDuration = Future.delayed(const Duration(seconds: 2));

  await envLoaded;
  final token = await tokenRead;

  // After Env.load, which holds the Firebase settings, and before runApp so
  // a notification arriving immediately already has its channel. A no-op
  // when push isn't configured, and it never throws — see the service.
  await NotificationService.initialize();
  // Only with a session in hand: the api takes the device's owner from the
  // bearer token, so registering while signed out would be rejected. The
  // login page does the same once a session is opened.
  if (token != null) unawaited(NotificationService.registerDevice());

  await minSplashDuration;

  runApp(QueelApp(initialPage: token != null ? const SearchPage() : const LoginPage()));
  FlutterNativeSplash.remove();
}
