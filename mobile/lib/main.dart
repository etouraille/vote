import 'package:flutter/material.dart';
import 'package:flutter_dotenv/flutter_dotenv.dart';
import 'package:flutter_native_splash/flutter_native_splash.dart';

Future<void> main() async {
  final widgetsBinding = WidgetsFlutterBinding.ensureInitialized();
  // Keeps the native splash (see pubspec.yaml's flutter_native_splash config
  // — the same queel.png the OS already shows before Flutter is even up) on
  // screen past Flutter's first frame, instead of it being auto-dismissed
  // and replaced by a second, differently-sized logo drawn by our own UI.
  FlutterNativeSplash.preserve(widgetsBinding: widgetsBinding);

  // dotenv.load finishes near-instantly, which would otherwise let the
  // splash vanish after a single frame — wait for both it and a 2s minimum
  // so it's actually visible, without adding to the wait on a slower load.
  // Done before runApp so HomePage's very first build already has
  // API_BASE_URL loaded, since it never rebuilds on its own afterward.
  await Future.wait([
    dotenv.load(fileName: 'lib/config/.env'),
    Future.delayed(const Duration(seconds: 2)),
  ]);

  runApp(const QueelApp());
  FlutterNativeSplash.remove();
}

class QueelApp extends StatelessWidget {
  const QueelApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'Queel',
      theme: ThemeData(colorScheme: ColorScheme.fromSeed(seedColor: Colors.deepPurple)),
      home: const HomePage(),
    );
  }
}

class HomePage extends StatelessWidget {
  const HomePage({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Queel')),
      body: Center(
        child: Text('API: ${dotenv.env['API_BASE_URL']}'),
      ),
    );
  }
}
