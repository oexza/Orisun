import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:orisun_flutter/orisun_flutter.dart';

void main() {
  test('saves, reads, queries, indexes, and reports CCC conflicts', () async {
    final directory = await Directory.systemTemp.createTemp('orisun-flutter-');
    final store = await OrisunStore.open(
      directory: directory.path,
      boundaries: const ['accounts'],
    );
    addTearDown(() async {
      await store.close();
      await directory.delete(recursive: true);
    });

    const query = OrisunQuery([
      OrisunCriterion([OrisunTag('accountId', 'account-1')]),
    ]);
    const event = OrisunEventInput(
      id: 'event-1',
      type: 'AccountOpened',
      data: {'accountId': 'account-1', 'owner': 'Ada'},
      metadata: {'deviceId': 'desktop-test'},
    );

    await store.createBoundaryIndex('accounts', 'account_id', const [
      OrisunIndexField(jsonKey: 'accountId', valueType: 'text'),
    ]);
    final position = await store.saveEvents(
      'accounts',
      const [event],
      expectedPosition: const OrisunPosition.notExists(),
      query: query,
    );
    expect(position.commitPosition, greaterThanOrEqualTo(0));

    final events = await store.getEvents('accounts');
    expect(events, hasLength(1));
    expect(events.single.id, 'event-1');
    expect(events.single.data, containsPair('accountId', 'account-1'));
    expect(events.single.data, containsPair('owner', 'Ada'));

    final latest = await store.getLatestByCriteria('accounts', query);
    expect(latest.matches.single.found, isTrue);
    expect(latest.matches.single.event?.id, 'event-1');

    await expectLater(
      store.saveEvents(
        'accounts',
        const [event],
        expectedPosition: const OrisunPosition.notExists(),
        query: query,
      ),
      throwsA(isA<OrisunConflictException>()),
    );
    await store.dropBoundaryIndex('accounts', 'account_id');
  });

  test('turns native subscriptions into Dart streams', () async {
    final directory = await Directory.systemTemp.createTemp('orisun-flutter-');
    final store = await OrisunStore.open(
      directory: directory.path,
      boundaries: const ['accounts'],
    );
    addTearDown(() async {
      await store.close();
      await directory.delete(recursive: true);
    });

    final nextEvent = store
        .subscribe(
          'accounts',
          subscriberName: 'dart-test',
          afterPosition: const OrisunPosition.notExists(),
        )
        .first
        .timeout(const Duration(seconds: 5));

    await store.saveEvents('accounts', const [
      OrisunEventInput(
        id: 'event-1',
        type: 'AccountOpened',
        data: {'accountId': 'account-1'},
      ),
    ]);
    expect((await nextEvent).id, 'event-1');
  });
}
