import 'dart:async';

import 'package:flutter/material.dart';
import 'package:orisun_flutter/orisun_flutter.dart';
import 'package:path_provider/path_provider.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  final supportDirectory = await getApplicationSupportDirectory();
  final store = await OrisunStore.open(
    directory: '${supportDirectory.path}/orisun',
    boundaries: const ['local'],
  );
  runApp(OrisunExample(store: store));
}

class OrisunExample extends StatelessWidget {
  const OrisunExample({required this.store, super.key});

  final OrisunStore store;

  @override
  Widget build(BuildContext context) => MaterialApp(
    title: 'Orisun Embedded',
    theme: ThemeData(colorSchemeSeed: Colors.deepOrange),
    home: EventLogPage(store: store),
  );
}

class EventLogPage extends StatefulWidget {
  const EventLogPage({required this.store, super.key});

  final OrisunStore store;

  @override
  State<EventLogPage> createState() => _EventLogPageState();
}

class _EventLogPageState extends State<EventLogPage> {
  final List<OrisunEvent> _events = [];
  StreamSubscription<OrisunEvent>? _subscription;
  Object? _error;

  @override
  void initState() {
    super.initState();
    _subscription = widget.store
        .subscribe(
          'local',
          subscriberName: 'example-ui',
          afterPosition: const OrisunPosition.notExists(),
        )
        .listen(
          (event) => setState(() {
            _events.add(event);
            _error = null;
          }),
          onError: (Object error) => setState(() => _error = error),
        );
  }

  Future<void> _appendEvent() async {
    final now = DateTime.now().toUtc();
    try {
      await widget.store.saveEvents('local', [
        OrisunEventInput(
          id: 'local-${now.microsecondsSinceEpoch}',
          type: 'LocalButtonPressed',
          data: {
            'sequence': _events.length + 1,
            'pressedAt': now.toIso8601String(),
          },
          metadata: const {'source': 'flutter-example'},
        ),
      ]);
    } catch (error) {
      setState(() => _error = error);
    }
  }

  @override
  void dispose() {
    unawaited(_subscription?.cancel());
    unawaited(widget.store.close());
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('Orisun embedded SQLite')),
    body: Column(
      children: [
        if (_error != null)
          MaterialBanner(
            content: Text('$_error'),
            actions: [
              TextButton(
                onPressed: () => setState(() => _error = null),
                child: const Text('Dismiss'),
              ),
            ],
          ),
        Expanded(
          child: _events.isEmpty
              ? const Center(child: Text('Append the first local event.'))
              : ListView.builder(
                  itemCount: _events.length,
                  itemBuilder: (context, index) {
                    final event = _events[index];
                    return ListTile(
                      leading: Text('${event.position.preparePosition}'),
                      title: Text(event.type),
                      subtitle: Text('${event.data}'),
                    );
                  },
                ),
        ),
      ],
    ),
    floatingActionButton: FloatingActionButton.extended(
      onPressed: _appendEvent,
      icon: const Icon(Icons.add),
      label: const Text('Append event'),
    ),
  );
}
