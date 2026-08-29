import 'package:flutter/material.dart';

import '../../../../app/theme/colors.dart';
import '../../../../core/network/exceptions.dart';
import '../../data/datasources/auth_api.dart';

/// The same password rule the web front applies. Enforced here rather than
/// left to the api, which only asks for eight characters: a rule that
/// differs between the two clients is one someone meets on one and fails
/// on the other, with no way to tell why.
final _passwordPattern = RegExp(r'^(?=.*[a-z])(?=.*[A-Z])(?=.*\d).{8,}$');

/// Creating an account, reached from the sign-in screen.
///
/// Ends on a message rather than on a session: the api sends a
/// confirmation email and refuses to sign in an account whose link has not
/// been followed, so pretending to log the visitor in here would only
/// produce a refusal they could not explain.
class RegisterPage extends StatefulWidget {
  const RegisterPage({super.key});

  @override
  State<RegisterPage> createState() => _RegisterPageState();
}

class _RegisterPageState extends State<RegisterPage> {
  final _emailController = TextEditingController();
  final _pseudoController = TextEditingController();
  final _passwordController = TextEditingController();
  final _confirmController = TextEditingController();

  bool _submitting = false;
  String? _error;

  /// The address the account was created for, and the signal that it was:
  /// null while the form is still being filled in.
  String? _registered;

  @override
  void initState() {
    super.initState();
    // The two rules are shown as they are met, not on submit — a form that
    // waits until the button is pressed to say what it wanted is a form
    // that makes you guess twice.
    _passwordController.addListener(_onFieldChanged);
    _confirmController.addListener(_onFieldChanged);
  }

  void _onFieldChanged() => setState(() {});

  @override
  void dispose() {
    _emailController.dispose();
    _pseudoController.dispose();
    _passwordController.dispose();
    _confirmController.dispose();
    super.dispose();
  }

  bool get _passwordValid => _passwordPattern.hasMatch(_passwordController.text);

  bool get _passwordsMatch =>
      _passwordController.text.isNotEmpty && _passwordController.text == _confirmController.text;

  bool get _canSubmit => _passwordValid && _passwordsMatch && !_submitting;

  Future<void> _submit() async {
    if (!_canSubmit) return;
    setState(() {
      _submitting = true;
      _error = null;
    });

    final email = _emailController.text.trim();
    try {
      await AuthApi.register(email, _pseudoController.text.trim(), _passwordController.text);
      if (mounted) setState(() => _registered = email);
    } on ApiException catch (e) {
      // The api's own wording: it is the one that knows the address is
      // already taken, and rewording it here could only say less.
      if (mounted) setState(() => _error = e.message);
    } catch (_) {
      if (mounted) setState(() => _error = 'Inscription impossible.');
    } finally {
      if (mounted) setState(() => _submitting = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      // The gradient the sign-in screen carries, for the same reason: the
      // two screens someone meets before having an account are the ones
      // that say which product this is.
      extendBodyBehindAppBar: true,
      appBar: AppBar(
        title: const Text('Inscription'),
        backgroundColor: Colors.transparent,
        foregroundColor: Colors.white,
      ),
      body: Container(
        decoration: const BoxDecoration(
          gradient: LinearGradient(
            begin: Alignment.topLeft,
            end: Alignment.bottomRight,
            colors: [AppColors.gradientStart, AppColors.gradientEnd],
          ),
        ),
        child: SafeArea(
          child: Scaffold(
            backgroundColor: Colors.transparent,
            // Scrollable for the same reason as the sign-in form: four
            // fields and an open keyboard overflow a small screen, and a
            // column that scrolls beats one that stripes its bottom.
            body: LayoutBuilder(
              builder: (context, constraints) => SingleChildScrollView(
                padding: const EdgeInsets.all(24),
                child: ConstrainedBox(
                  constraints: BoxConstraints(minHeight: constraints.maxHeight - 48),
                  child: Column(
                    mainAxisAlignment: MainAxisAlignment.center,
                    children: [
                      Card(
                        child: Padding(
                          padding: const EdgeInsets.all(20),
                          child: _registered == null ? _form() : _confirmationNotice(),
                        ),
                      ),
                    ],
                  ),
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }

  Widget _form() {
    final theme = Theme.of(context);

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      mainAxisSize: MainAxisSize.min,
      children: [
        Text('Créez votre compte pour voter.', style: theme.textTheme.bodyMedium),
        const SizedBox(height: 20),
        TextField(
          controller: _emailController,
          keyboardType: TextInputType.emailAddress,
          autocorrect: false,
          decoration: const InputDecoration(labelText: 'Email'),
        ),
        const SizedBox(height: 12),
        TextField(
          controller: _pseudoController,
          // The api caps it at 50 runes and answers 400 past that; stopping
          // the field there turns a refusal into a key that does nothing.
          maxLength: 50,
          decoration: const InputDecoration(
            labelText: 'Pseudo (optionnel)',
            helperText: 'Le nom affiché à côté de vos votes.',
          ),
        ),
        const SizedBox(height: 4),
        TextField(
          controller: _passwordController,
          obscureText: true,
          decoration: const InputDecoration(labelText: 'Mot de passe'),
        ),
        const SizedBox(height: 6),
        Text(
          '8 caractères minimum, dont une minuscule, une majuscule et un chiffre.',
          style: theme.textTheme.bodySmall?.copyWith(
            // Red only once something has been typed: a rule shown in red
            // on an untouched field reads as a mistake already made.
            color: _passwordController.text.isEmpty || _passwordValid
                ? theme.colorScheme.onSurfaceVariant
                : AppColors.removedInk,
          ),
        ),
        const SizedBox(height: 12),
        TextField(
          controller: _confirmController,
          obscureText: true,
          decoration: const InputDecoration(labelText: 'Confirmer le mot de passe'),
          onSubmitted: (_) => _submit(),
        ),
        if (_confirmController.text.isNotEmpty && !_passwordsMatch) ...[
          const SizedBox(height: 6),
          Text(
            'Les mots de passe ne correspondent pas.',
            style: theme.textTheme.bodySmall?.copyWith(color: AppColors.removedInk),
          ),
        ],
        if (_error != null) ...[
          const SizedBox(height: 16),
          Text(_error!, style: TextStyle(color: AppColors.removedInk)),
        ],
        const SizedBox(height: 20),
        FilledButton(
          onPressed: _canSubmit ? _submit : null,
          child: Text(_submitting ? 'Inscription…' : "S'inscrire"),
        ),
        const SizedBox(height: 8),
        TextButton(
          onPressed: _submitting ? null : () => Navigator.of(context).pop(),
          child: const Text('Déjà un compte ? Se connecter'),
        ),
      ],
    );
  }

  /// What replaces the form once the account exists: the account cannot be
  /// used yet, and saying so is the only useful thing left to do here.
  Widget _confirmationNotice() {
    final theme = Theme.of(context);

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      mainAxisSize: MainAxisSize.min,
      children: [
        Text('Vérifiez votre boîte mail', style: theme.textTheme.titleMedium),
        const SizedBox(height: 8),
        Text(
          'Un email de confirmation a été envoyé à $_registered. '
          'Cliquez sur le lien qu\'il contient pour activer votre compte.',
          style: theme.textTheme.bodyMedium,
        ),
        const SizedBox(height: 20),
        FilledButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('Retour à la connexion'),
        ),
      ],
    );
  }
}
