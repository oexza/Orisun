final class OrisunPosition {
  const OrisunPosition({
    required this.commitPosition,
    required this.preparePosition,
  });

  const OrisunPosition.notExists() : commitPosition = -1, preparePosition = -1;

  final int commitPosition;
  final int preparePosition;

  Map<String, Object?> toJson() => {
    'commit_position': commitPosition,
    'prepare_position': preparePosition,
  };

  factory OrisunPosition.fromJson(Map<String, Object?> json) => OrisunPosition(
    commitPosition: json['commit_position']! as int,
    preparePosition: json['prepare_position']! as int,
  );
}

final class OrisunEventInput {
  const OrisunEventInput({
    required this.id,
    required this.type,
    required this.data,
    this.metadata,
  });

  final String id;
  final String type;
  final Object? data;
  final Object? metadata;

  Map<String, Object?> toJson() => {
    'event_id': id,
    'event_type': type,
    'data': data,
    'metadata': metadata,
  };
}

final class OrisunEvent {
  const OrisunEvent({
    required this.id,
    required this.type,
    required this.data,
    required this.metadata,
    required this.position,
    required this.dateCreated,
  });

  final String id;
  final String type;
  final Object? data;
  final Object? metadata;
  final OrisunPosition position;
  final DateTime dateCreated;

  factory OrisunEvent.fromJson(Map<String, Object?> json) => OrisunEvent(
    id: json['event_id']! as String,
    type: json['event_type']! as String,
    data: json['data'],
    metadata: json['metadata'],
    position: OrisunPosition.fromJson(
      (json['position']! as Map).cast<String, Object?>(),
    ),
    dateCreated: DateTime.parse(json['date_created']! as String),
  );
}

final class OrisunTag {
  const OrisunTag(this.key, this.value);

  final String key;
  final String value;

  Map<String, Object?> toJson() => {'key': key, 'value': value};
}

final class OrisunCriterion {
  const OrisunCriterion(this.tags);

  final List<OrisunTag> tags;

  Map<String, Object?> toJson() => {
    'tags': tags.map((tag) => tag.toJson()).toList(growable: false),
  };
}

final class OrisunQuery {
  const OrisunQuery(this.criteria);

  final List<OrisunCriterion> criteria;

  Map<String, Object?> toJson() => {
    'criteria': criteria
        .map((criterion) => criterion.toJson())
        .toList(growable: false),
  };
}

final class OrisunLatestMatch {
  const OrisunLatestMatch({required this.found, this.event});

  final bool found;
  final OrisunEvent? event;

  factory OrisunLatestMatch.fromJson(Map<String, Object?> json) =>
      OrisunLatestMatch(
        found: json['found']! as bool,
        event: json['event'] == null
            ? null
            : OrisunEvent.fromJson(
                (json['event']! as Map).cast<String, Object?>(),
              ),
      );
}

final class OrisunLatestResult {
  const OrisunLatestResult({
    required this.matches,
    required this.contextPosition,
  });

  final List<OrisunLatestMatch> matches;
  final OrisunPosition contextPosition;

  factory OrisunLatestResult.fromJson(Map<String, Object?> json) =>
      OrisunLatestResult(
        matches: (json['matches']! as List)
            .map(
              (value) => OrisunLatestMatch.fromJson(
                (value as Map).cast<String, Object?>(),
              ),
            )
            .toList(growable: false),
        contextPosition: OrisunPosition.fromJson(
          (json['context_position']! as Map).cast<String, Object?>(),
        ),
      );
}

final class OrisunIndexField {
  const OrisunIndexField({required this.jsonKey, required this.valueType});

  final String jsonKey;
  final String valueType;

  Map<String, Object?> toJson() => {
    'json_key': jsonKey,
    'value_type': valueType,
  };
}

final class OrisunIndexCondition {
  const OrisunIndexCondition({
    required this.key,
    required this.operator,
    required this.value,
  });

  final String key;
  final String operator;
  final String value;

  Map<String, Object?> toJson() => {
    'key': key,
    'operator': operator,
    'value': value,
  };
}

enum OrisunReadDirection { ascending, descending }

final class OrisunException implements Exception {
  const OrisunException(this.code, this.message);

  final String code;
  final String message;

  @override
  String toString() => 'OrisunException($code): $message';
}

final class OrisunConflictException extends OrisunException {
  const OrisunConflictException(String message)
    : super('already_exists', message);
}
