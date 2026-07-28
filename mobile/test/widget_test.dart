import 'package:flutter_test/flutter_test.dart';

import 'package:mobile/app/app.dart';
import 'package:mobile/features/authentication/presentation/pages/login_page.dart';

void main() {
  testWidgets('shows the login page when no session is stored', (WidgetTester tester) async {
    await tester.pumpWidget(const QueelApp(initialPage: LoginPage()));

    expect(find.text('Connexion'), findsOneWidget);
  });
}
