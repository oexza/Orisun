import 'dart:convert';

import 'models.dart';
import 'native_executor.dart';

const _supportedABIVersion = 1;

final class OrisunStore {
  OrisunStore._(this._handle);

  final int _handle;
  bool _closed = false;

  static Future<OrisunStore> open({
    required String directory,
    required List<String> boundaries,
  }) async {
    if (directory.isEmpty) {
      throw const OrisunException(
        'invalid_argument',
        'directory must not be empty',
      );
    }
    if (boundaries.isEmpty || boundaries.any((boundary) => boundary.isEmpty)) {
      throw const OrisunException(
        'invalid_argument',
        'boundaries must contain at least one non-empty name',
      );
    }
    final abiVersion = await NativeExecutor.instance.call('abiVersion');
    if (abiVersion != _supportedABIVersion) {
      throw OrisunException(
        'abi_mismatch',
        'Dart supports ABI $_supportedABIVersion but the native library reports $abiVersion',
      );
    }
    final response = await NativeExecutor.instance.call('open', {
      'directory': directory,
      'boundaries': jsonEncode(boundaries),
    });
    final envelope = _unwrapEnvelope(response);
    return OrisunStore._(envelope['handle']! as int);
  }

  Future<OrisunPosition> saveEvents(
    String boundary,
    List<OrisunEventInput> events, {
    OrisunPosition? expectedPosition,
    OrisunQuery? query,
  }) async {
    _requireOpen();
    final envelope = _unwrapEnvelope(
      await NativeExecutor.instance.call('saveEvents', {
        'store': _handle,
        'boundary': boundary,
        'events': jsonEncode(events.map((event) => event.toJson()).toList()),
        'expectedPosition': _optionalJson(expectedPosition?.toJson()),
        'query': _optionalJson(query?.toJson()),
      }),
    );
    return OrisunPosition.fromJson(
      (envelope['value']! as Map).cast<String, Object?>(),
    );
  }

  Future<List<OrisunEvent>> getEvents(
    String boundary, {
    OrisunPosition? fromPosition,
    OrisunQuery? query,
    int count = 100,
    OrisunReadDirection direction = OrisunReadDirection.ascending,
  }) async {
    _requireOpen();
    final envelope = _unwrapEnvelope(
      await NativeExecutor.instance.call('getEvents', {
        'store': _handle,
        'boundary': boundary,
        'fromPosition': _optionalJson(fromPosition?.toJson()),
        'query': _optionalJson(query?.toJson()),
        'count': count,
        'descending': direction == OrisunReadDirection.descending,
      }),
    );
    return (envelope['value']! as List)
        .map(
          (value) =>
              OrisunEvent.fromJson((value as Map).cast<String, Object?>()),
        )
        .toList(growable: false);
  }

  Future<OrisunLatestResult> getLatestByCriteria(
    String boundary,
    OrisunQuery query,
  ) async {
    _requireOpen();
    final envelope = _unwrapEnvelope(
      await NativeExecutor.instance.call('getLatestByCriteria', {
        'store': _handle,
        'boundary': boundary,
        'query': jsonEncode(query.toJson()),
      }),
    );
    return OrisunLatestResult.fromJson(
      (envelope['value']! as Map).cast<String, Object?>(),
    );
  }

  Future<void> createBoundaryIndex(
    String boundary,
    String name,
    List<OrisunIndexField> fields, {
    List<OrisunIndexCondition> conditions = const [],
    String combinator = 'AND',
  }) async {
    _requireOpen();
    _unwrapEnvelope(
      await NativeExecutor.instance.call('createBoundaryIndex', {
        'store': _handle,
        'boundary': boundary,
        'name': name,
        'fields': jsonEncode(fields.map((field) => field.toJson()).toList()),
        'conditions': jsonEncode(
          conditions.map((condition) => condition.toJson()).toList(),
        ),
        'combinator': combinator,
      }),
    );
  }

  Future<void> dropBoundaryIndex(String boundary, String name) async {
    _requireOpen();
    _unwrapEnvelope(
      await NativeExecutor.instance.call('dropBoundaryIndex', {
        'store': _handle,
        'boundary': boundary,
        'name': name,
      }),
    );
  }

  Stream<OrisunEvent> subscribe(
    String boundary, {
    required String subscriberName,
    OrisunPosition? afterPosition,
    OrisunQuery? query,
  }) async* {
    _requireOpen();
    final response = _unwrapEnvelope(
      await NativeExecutor.instance.call('subscribe', {
        'store': _handle,
        'boundary': boundary,
        'subscriberName': subscriberName,
        'afterPosition': _optionalJson(afterPosition?.toJson()),
        'query': _optionalJson(query?.toJson()),
      }),
    );
    final subscriptionHandle = response['handle']! as int;
    try {
      await for (final envelope in NativeExecutor.instance.subscriptionMessages(
        subscriptionHandle,
      )) {
        if (envelope['ok'] != true) {
          if (_closed) {
            break;
          }
          _unwrapEnvelope(envelope);
        }
        switch (envelope['kind']) {
          case 'event':
            yield OrisunEvent.fromJson(
              (envelope['value']! as Map).cast<String, Object?>(),
            );
          case 'error':
            throw OrisunException(
              'subscription_error',
              envelope['value']! as String,
            );
          case 'done':
            return;
          case 'timeout':
            continue;
        }
      }
    } finally {
      if (!_closed) {
        try {
          _unwrapEnvelope(
            await NativeExecutor.instance.call('stopSubscription', {
              'subscription': subscriptionHandle,
            }),
          );
        } on OrisunException {
          // The native store may have completed and released it first.
        }
      }
    }
  }

  Future<void> close() async {
    if (_closed) {
      return;
    }
    _closed = true;
    _unwrapEnvelope(
      await NativeExecutor.instance.call('close', {'store': _handle}),
    );
  }

  void _requireOpen() {
    if (_closed) {
      throw const OrisunException('closed', 'store is closed');
    }
  }
}

String _optionalJson(Object? value) => value == null ? '' : jsonEncode(value);

Map<String, Object?> _unwrapEnvelope(Object? value) {
  final envelope = (value as Map).cast<String, Object?>();
  if (envelope['ok'] == true) {
    return envelope;
  }
  final error = (envelope['error']! as Map).cast<String, Object?>();
  final code = error['code']! as String;
  final message = error['message']! as String;
  if (code == 'already_exists') {
    throw OrisunConflictException(message);
  }
  throw OrisunException(code, message);
}
