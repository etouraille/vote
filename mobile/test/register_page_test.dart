import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:mobile/app/app.dart';
import 'package:mobile/features/authentication/presentation/pages/register_page.dart';

/// The rule the button enforces is the web front's, not the api's looser
/// one (eight characters, nothing else). It is pinned here because the two
/// clients drifting apart is invisible until someone meets a password one
/// of them accepts and the other refuses.
void main() {
  Future<void> fill(WidgetTester tester, String password, String confirmation) async {
    await tester.enterText(find.widgetWithText(TextField, 'Mot de passe'), password);
    await tester.enterText(
      find.widgetWithText(TextField, 'Confirmer le mot de passe'),
      confirmation,
    );
    await tester.pump();
  }

  bool submitEnabled(WidgetTester tester) {
    final button = tester.widget<FilledButton>(find.widgetWithText(FilledButton, "S'inscrire"));
    return button.onPressed != null;
  }

  testWidgets('refuses a password the front would refuse', (tester) async {
    await tester.pumpWidget(const QueelApp(initialPage: RegisterPage()));

    // Long enough for the api, missing the digit and the capital.
    await fill(tester, 'motdepasse', 'motdepasse');
    expect(submitEnabled(tester), isFalse);
    expect(find.textContaining('une majuscule'), findsOneWidget);
  });

  testWidgets('refuses two passwords that differ', (tester) async {
    await tester.pumpWidget(const QueelApp(initialPage: RegisterPage()));

    await fill(tester, 'MotDePasse1', 'MotDePasse2');
    expect(submitEnabled(tester), isFalse);
    expect(find.text('Les mots de passe ne correspondent pas.'), findsOneWidget);
  });

  testWidgets('accepts a password meeting every rule', (tester) async {
    await tester.pumpWidget(const QueelApp(initialPage: RegisterPage()));

    await fill(tester, 'MotDePasse1', 'MotDePasse1');
    expect(submitEnabled(tester), isTrue);
    expect(find.text('Les mots de passe ne correspondent pas.'), findsNothing);
  });
}
