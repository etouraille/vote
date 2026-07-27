import 'package:flutter_dotenv/flutter_dotenv.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:mobile/app/app.dart';

void main() {
  testWidgets('shows the home page', (WidgetTester tester) async {
    // main() normally loads lib/config/.env before runApp — pumpWidget here
    // bypasses main() entirely, so HomePage would otherwise read dotenv
    // before it's ever been initialized.
    dotenv.loadFromString(envString: 'API_BASE_URL=http://localhost:8080');

    await tester.pumpWidget(const QueelApp());

    expect(find.text('Queel'), findsOneWidget);
  });
}
