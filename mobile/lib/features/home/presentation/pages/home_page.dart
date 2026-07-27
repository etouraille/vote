import 'package:flutter/material.dart';

import '../../../../app/config/env.dart';

class HomePage extends StatelessWidget {
  const HomePage({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Queel')),
      body: Center(
        child: Text('API: ${Env.apiBaseUrl}'),
      ),
    );
  }
}
