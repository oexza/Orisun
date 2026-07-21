import 'dart:async';
import 'dart:convert';
import 'dart:ffi';
import 'dart:io';
import 'dart:isolate';

import 'package:ffi/ffi.dart';
import 'package:flutter/services.dart';

import '../orisun_flutter_bindings_generated.dart' as native;

final class NativeExecutor {
  NativeExecutor._();

  static final NativeExecutor instance = NativeExecutor._();

  final Map<int, Completer<Object?>> _pending = {};
  int _nextRequestID = 1;
  Future<SendPort>? _worker;
  static const _mobileChannel = MethodChannel(
    'com.orisunlabs.orisun_flutter/methods',
  );

  bool get usesPlatformChannel => Platform.isAndroid || Platform.isIOS;

  Future<Object?> call(
    String operation, [
    Map<String, Object?> arguments = const {},
  ]) async {
    if (usesPlatformChannel) {
      final response = await _mobileChannel.invokeMethod<Object?>(
        operation,
        arguments,
      );
      if (response is String) {
        return (jsonDecode(response) as Map).cast<String, Object?>();
      }
      return response;
    }
    final worker = await (_worker ??= _start());
    final id = _nextRequestID++;
    final completer = Completer<Object?>();
    _pending[id] = completer;
    worker.send({'id': id, 'operation': operation, 'arguments': arguments});
    return completer.future;
  }

  Stream<Map<String, Object?>> subscriptionMessages(int handle) async* {
    if (usesPlatformChannel) {
      while (true) {
        final response =
            (await call('subscriptionNext', {
                      'subscription': handle,
                      'timeoutMillis': 250,
                    })
                    as Map)
                .cast<String, Object?>();
        yield response;
        if (response['ok'] != true || response['kind'] == 'done') {
          return;
        }
      }
    } else {
      final receivePort = ReceivePort();
      final isolate = await Isolate.spawn(subscriptionWorker, <Object?>[
        receivePort.sendPort,
        handle,
      ]);
      try {
        await for (final message in receivePort) {
          final response = (message as Map).cast<String, Object?>();
          yield response;
          if (response['ok'] != true || response['kind'] == 'done') {
            return;
          }
        }
      } finally {
        receivePort.close();
        isolate.kill(priority: Isolate.immediate);
      }
    }
  }

  Future<SendPort> _start() async {
    final ready = Completer<SendPort>();
    final receivePort = ReceivePort();
    receivePort.listen((message) {
      if (message is SendPort) {
        ready.complete(message);
        return;
      }
      final response = (message as Map).cast<String, Object?>();
      final id = response['id']! as int;
      final completer = _pending.remove(id);
      if (completer == null) {
        return;
      }
      final workerError = response['workerError'];
      if (workerError != null) {
        completer.completeError(StateError(workerError as String));
      } else {
        completer.complete(response['result']);
      }
    });
    await Isolate.spawn(_workerMain, receivePort.sendPort);
    return ready.future;
  }
}

void _workerMain(SendPort parent) {
  final requests = ReceivePort();
  parent.send(requests.sendPort);
  requests.listen((message) {
    final request = (message as Map).cast<String, Object?>();
    final id = request['id']! as int;
    try {
      final result = _dispatch(
        request['operation']! as String,
        (request['arguments']! as Map).cast<String, Object?>(),
      );
      parent.send({'id': id, 'result': result});
    } catch (error, stackTrace) {
      parent.send({'id': id, 'workerError': '$error\n$stackTrace'});
    }
  });
}

Object? _dispatch(String operation, Map<String, Object?> arguments) {
  switch (operation) {
    case 'abiVersion':
      return native.orisun_abi_version();
    case 'open':
      return _callWithStrings([
        arguments.string('directory'),
        arguments.string('boundaries'),
      ], (values) => native.orisun_open(values[0], values[1]));
    case 'close':
      return _decodeAndFree(native.orisun_close(arguments.integer('store')));
    case 'saveEvents':
      return _callWithStrings(
        [
          arguments.string('boundary'),
          arguments.string('events'),
          arguments.string('expectedPosition'),
          arguments.string('query'),
        ],
        (values) => native.orisun_save_events(
          arguments.integer('store'),
          values[0],
          values[1],
          values[2],
          values[3],
        ),
      );
    case 'getEvents':
      return _callWithStrings(
        [
          arguments.string('boundary'),
          arguments.string('fromPosition'),
          arguments.string('query'),
        ],
        (values) => native.orisun_get_events(
          arguments.integer('store'),
          values[0],
          values[1],
          values[2],
          arguments.integer('count'),
          arguments.boolean('descending') ? 1 : 0,
        ),
      );
    case 'getLatestByCriteria':
      return _callWithStrings(
        [arguments.string('boundary'), arguments.string('query')],
        (values) => native.orisun_get_latest_by_criteria(
          arguments.integer('store'),
          values[0],
          values[1],
        ),
      );
    case 'createBoundaryIndex':
      return _callWithStrings(
        [
          arguments.string('boundary'),
          arguments.string('name'),
          arguments.string('fields'),
          arguments.string('conditions'),
          arguments.string('combinator'),
        ],
        (values) => native.orisun_create_boundary_index(
          arguments.integer('store'),
          values[0],
          values[1],
          values[2],
          values[3],
          values[4],
        ),
      );
    case 'dropBoundaryIndex':
      return _callWithStrings(
        [arguments.string('boundary'), arguments.string('name')],
        (values) => native.orisun_drop_boundary_index(
          arguments.integer('store'),
          values[0],
          values[1],
        ),
      );
    case 'subscribe':
      return _callWithStrings(
        [
          arguments.string('boundary'),
          arguments.string('subscriberName'),
          arguments.string('afterPosition'),
          arguments.string('query'),
        ],
        (values) => native.orisun_subscribe(
          arguments.integer('store'),
          values[0],
          values[1],
          values[2],
          values[3],
        ),
      );
    case 'stopSubscription':
      return _decodeAndFree(
        native.orisun_subscription_stop(arguments.integer('subscription')),
      );
    default:
      throw ArgumentError.value(
        operation,
        'operation',
        'unknown native operation',
      );
  }
}

Map<String, Object?> _callWithStrings(
  List<String> strings,
  Pointer<Char> Function(List<Pointer<Char>>) invoke,
) {
  final allocated = strings.map((value) => value.toNativeUtf8()).toList();
  try {
    return _decodeAndFree(
      invoke(allocated.map((value) => value.cast<Char>()).toList()),
    );
  } finally {
    for (final value in allocated) {
      malloc.free(value);
    }
  }
}

Map<String, Object?> _decodeAndFree(Pointer<Char> result) {
  if (result == nullptr) {
    throw StateError('Orisun returned a null response');
  }
  try {
    final decoded = jsonDecode(result.cast<Utf8>().toDartString());
    return (decoded as Map).cast<String, Object?>();
  } finally {
    native.orisun_free_string(result);
  }
}

void subscriptionWorker(List<Object?> arguments) {
  final parent = arguments[0]! as SendPort;
  final handle = arguments[1]! as int;
  while (true) {
    final response = _decodeAndFree(
      native.orisun_subscription_next(handle, 250),
    );
    parent.send(response);
    if (response['ok'] != true || response['kind'] == 'done') {
      break;
    }
  }
}

extension on Map<String, Object?> {
  String string(String key) => this[key]! as String;
  int integer(String key) => this[key]! as int;
  bool boolean(String key) => this[key]! as bool;
}
