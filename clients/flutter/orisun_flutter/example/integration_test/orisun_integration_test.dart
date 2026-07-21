import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:orisun_flutter/orisun_flutter.dart';

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('writes, reads, and streams through the platform bridge', (
    tester,
  ) async {
    final directory = await Directory.systemTemp.createTemp(
      'orisun-flutter-device-',
    );
    final store = await OrisunStore.open(
      directory: directory.path,
      boundaries: const ['integration'],
    );
    addTearDown(() async {
      await store.close();
      await directory.delete(recursive: true);
    });

    final delivered = store
        .subscribe(
          'integration',
          subscriberName: 'device-test',
          afterPosition: const OrisunPosition.notExists(),
        )
        .first
        .timeout(const Duration(seconds: 10));

    const input = OrisunEventInput(
      id: 'device-event-1',
      type: 'DeviceEventCreated',
      data: {'device': true},
    );
    final position = await store.saveEvents('integration', const [input]);
    final event = await delivered;
    final page = await store.getEvents('integration');

    expect(position.commitPosition, event.position.commitPosition);
    expect(position.preparePosition, event.position.preparePosition);
    expect(event.id, input.id);
    expect(page.map((value) => value.id), contains(input.id));
  });
}
