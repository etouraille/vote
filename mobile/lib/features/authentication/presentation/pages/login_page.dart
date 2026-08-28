import 'dart:async';

import 'package:flutter/material.dart';
import 'package:google_sign_in/google_sign_in.dart';

import '../../../../app/router.dart';
import '../../../../core/network/exceptions.dart';
import '../../../../core/storage/secure_storage.dart';
import '../../../notifications/notification_service.dart';
import '../../data/datasources/auth_api.dart';
import '../../data/datasources/google_sign_in_api.dart';
import '../../data/models/google_login_result.dart';
import '../../data/models/session.dart';

class LoginPage extends StatefulWidget {
  const LoginPage({super.key});

  @override
  State<LoginPage> createState() => _LoginPageState();
}

class _LoginPageState extends State<LoginPage> {
  final _emailController = TextEditingController();
  final _passwordController = TextEditingController();
  bool _submitting = false;
  String? _error;

  @override
  void dispose() {
    _emailController.dispose();
    _passwordController.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    if (_submitting) return;
    setState(() {
      _submitting = true;
      _error = null;
    });

    try {
      final session = await AuthApi.login(_emailController.text.trim(), _passwordController.text);
      await _openSession(session);
    } on ApiException catch (e) {
      setState(() => _error = e.message);
    } catch (_) {
      setState(() => _error = 'Connexion impossible.');
    } finally {
      if (mounted) setState(() => _submitting = false);
    }
  }

  Future<void> _signInWithGoogle() async {
    if (_submitting) return;
    setState(() {
      _submitting = true;
      _error = null;
    });

    try {
      final idToken = await GoogleSignInApi.signInIdToken();
      if (idToken == null) return; // picker dismissed — nothing to report
      await _exchangeGoogleIdToken(idToken);
    } on ApiException catch (e) {
      setState(() => _error = e.message);
    } on GoogleSignInException catch (e) {
      // description carries whatever Play Services actually said — a bare
      // code.name is almost always `unknownError`, which names the symptom
      // and hides the cause (misconfigured OAuth client, unregistered
      // signing certificate, …).
      final detail = e.description?.trim();
      setState(
        () => _error = detail == null || detail.isEmpty
            ? 'Connexion Google impossible (${e.code.name}).'
            : 'Connexion Google impossible (${e.code.name}) : $detail',
      );
    } catch (_) {
      setState(() => _error = 'Connexion Google impossible.');
    } finally {
      if (mounted) setState(() => _submitting = false);
    }
  }

  /// Trades a verified Google ID token for a session, asking for a pseudo
  /// and retrying once if this Google account is new to the backend (see
  /// AuthApi.googleLogin).
  Future<void> _exchangeGoogleIdToken(String idToken) async {
    var result = await AuthApi.googleLogin(idToken);

    if (result is GoogleLoginNeedsPseudo) {
      if (!mounted) return;
      final pseudo = await _promptForPseudo();
      if (pseudo == null) return; // dialog dismissed
      result = await AuthApi.googleLogin(idToken, pseudo: pseudo);
    }

    switch (result) {
      case GoogleLoginSuccess(:final session):
        await _openSession(session);
      case GoogleLoginNeedsPseudo():
        setState(() => _error = 'Un pseudo est nécessaire pour finaliser l’inscription.');
    }
  }

  Future<void> _openSession(Session session) async {
    await SecureStorage.writeSession(session.token, session.expiresAt);
    // Now that there's a bearer token: the api reads this device's owner
    // from it, so registering any earlier would be refused. Not awaited —
    // notifications must not hold up the navigation.
    unawaited(NotificationService.registerDevice());
    if (!mounted) return;
    Navigator.of(context).pushReplacementNamed(AppRouter.articles);

    // After the replacement, so the text a notification pointed at is
    // pushed on top of the search page and the back button leads somewhere.
    // A no-op unless a tap launched the app while it was signed out.
    NotificationService.openPendingLaunch();
  }

  /// Returns the entered pseudo, or null if the user dismissed the dialog.
  Future<String?> _promptForPseudo() {
    final controller = TextEditingController();

    return showDialog<String>(
      context: context,
      builder: (context) {
        void submit() {
          final pseudo = controller.text.trim();
          if (pseudo.isEmpty) return;
          Navigator.of(context).pop(pseudo);
        }

        return AlertDialog(
          title: const Text('Choisissez un pseudo'),
          content: TextField(
            controller: controller,
            autofocus: true,
            decoration: const InputDecoration(labelText: 'Pseudo'),
            onSubmitted: (_) => submit(),
          ),
          actions: [
            TextButton(onPressed: () => Navigator.of(context).pop(), child: const Text('Annuler')),
            FilledButton(onPressed: submit, child: const Text('Continuer')),
          ],
        );
      },
    ).whenComplete(controller.dispose);
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Connexion')),
      // Scrollable because the Google button pushed the form tall enough
      // that an open keyboard overflows the remaining height — the column
      // scrolls instead of striping the bottom of the screen. The minHeight
      // is what keeps mainAxisAlignment.center meaningful: without it the
      // column would shrink-wrap its children and sit at the top.
      body: LayoutBuilder(
        builder: (context, constraints) => SingleChildScrollView(
          padding: const EdgeInsets.all(24),
          child: ConstrainedBox(
            constraints: BoxConstraints(minHeight: constraints.maxHeight - 48),
            child: Column(
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                TextField(
                  controller: _emailController,
                  keyboardType: TextInputType.emailAddress,
                  decoration: const InputDecoration(labelText: 'Email'),
                ),
                const SizedBox(height: 12),
                TextField(
                  controller: _passwordController,
                  obscureText: true,
                  decoration: const InputDecoration(labelText: 'Mot de passe'),
                  onSubmitted: (_) => _submit(),
                ),
                const SizedBox(height: 24),
                if (_error != null) ...[
                  Text(_error!, style: const TextStyle(color: Colors.red)),
                  const SizedBox(height: 12),
                ],
                FilledButton(
                  onPressed: _submitting ? null : _submit,
                  child: Text(_submitting ? 'Connexion…' : 'Se connecter'),
                ),
                // Hidden where google_sign_in can't drive its own UI (web), since
                // there the plugin requires a Google-rendered button instead.
                if (GoogleSignInApi.isSupported) ...[
                  const SizedBox(height: 24),
                  const Row(
                    children: [
                      Expanded(child: Divider()),
                      Padding(padding: EdgeInsets.symmetric(horizontal: 12), child: Text('ou')),
                      Expanded(child: Divider()),
                    ],
                  ),
                  const SizedBox(height: 16),
                  OutlinedButton(
                    onPressed: _submitting ? null : _signInWithGoogle,
                    child: const Text('Continuer avec Google'),
                  ),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }
}
