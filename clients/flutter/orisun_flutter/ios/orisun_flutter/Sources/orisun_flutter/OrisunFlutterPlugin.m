#import "OrisunFlutterPlugin.h"

#import <OrisunFlutterMobile/OrisunFlutterMobile.h>

@interface OrisunFlutterPlugin ()
@property(nonatomic, strong) OrisunFluttermobileBridge *bridge;
@end

@implementation OrisunFlutterPlugin

+ (void)registerWithRegistrar:(NSObject<FlutterPluginRegistrar> *)registrar {
  NSObject<FlutterBinaryMessenger> *messenger = registrar.messenger;
  FlutterMethodChannel *channel = [[FlutterMethodChannel alloc]
      initWithName:@"com.orisunlabs.orisun_flutter/methods"
      binaryMessenger:messenger
      codec:[FlutterStandardMethodCodec sharedInstance]
      taskQueue:[messenger makeBackgroundTaskQueue]];
  OrisunFlutterPlugin *instance = [[OrisunFlutterPlugin alloc] init];
  instance.bridge = OrisunFluttermobileNewBridge();
  [registrar addMethodCallDelegate:instance channel:channel];
}

- (void)handleMethodCall:(FlutterMethodCall *)call result:(FlutterResult)result {
  @try {
    NSDictionary<NSString *, id> *arguments = call.arguments ?: @{};
    OrisunFluttermobileBridge *bridge = self.bridge;
    if ([call.method isEqualToString:@"abiVersion"]) {
      result(@([bridge abiVersion]));
    } else if ([call.method isEqualToString:@"open"]) {
      result([bridge open:[self string:arguments key:@"directory"]
            boundariesJSON:[self string:arguments key:@"boundaries"]]);
    } else if ([call.method isEqualToString:@"close"]) {
      result([bridge close:[self integer:arguments key:@"store"]]);
    } else if ([call.method isEqualToString:@"saveEvents"]) {
      result([bridge saveEvents:[self integer:arguments key:@"store"]
                       boundary:[self string:arguments key:@"boundary"]
                     eventsJSON:[self string:arguments key:@"events"]
           expectedPositionJSON:[self string:arguments key:@"expectedPosition"]
                      queryJSON:[self string:arguments key:@"query"]]);
    } else if ([call.method isEqualToString:@"getEvents"]) {
      result([bridge getEvents:[self integer:arguments key:@"store"]
                      boundary:[self string:arguments key:@"boundary"]
              fromPositionJSON:[self string:arguments key:@"fromPosition"]
                     queryJSON:[self string:arguments key:@"query"]
                         count:(long)[self integer:arguments key:@"count"]
                    descending:[self boolean:arguments key:@"descending"]]);
    } else if ([call.method isEqualToString:@"getLatestByCriteria"]) {
      result([bridge getLatestByCriteria:[self integer:arguments key:@"store"]
                                 boundary:[self string:arguments key:@"boundary"]
                                queryJSON:[self string:arguments key:@"query"]]);
    } else if ([call.method isEqualToString:@"createBoundaryIndex"]) {
      result([bridge createBoundaryIndex:[self integer:arguments key:@"store"]
                                boundary:[self string:arguments key:@"boundary"]
                                    name:[self string:arguments key:@"name"]
                              fieldsJSON:[self string:arguments key:@"fields"]
                          conditionsJSON:[self string:arguments key:@"conditions"]
                              combinator:[self string:arguments key:@"combinator"]]);
    } else if ([call.method isEqualToString:@"dropBoundaryIndex"]) {
      result([bridge dropBoundaryIndex:[self integer:arguments key:@"store"]
                              boundary:[self string:arguments key:@"boundary"]
                                  name:[self string:arguments key:@"name"]]);
    } else if ([call.method isEqualToString:@"subscribe"]) {
      result([bridge subscribe:[self integer:arguments key:@"store"]
                       boundary:[self string:arguments key:@"boundary"]
                 subscriberName:[self string:arguments key:@"subscriberName"]
              afterPositionJSON:[self string:arguments key:@"afterPosition"]
                      queryJSON:[self string:arguments key:@"query"]]);
    } else if ([call.method isEqualToString:@"subscriptionNext"]) {
      int64_t subscription = [self integer:arguments key:@"subscription"];
      int64_t timeoutMillis = [self integer:arguments key:@"timeoutMillis"];
      dispatch_async(dispatch_get_global_queue(QOS_CLASS_USER_INITIATED, 0), ^{
        result([bridge subscriptionNext:subscription timeoutMillis:timeoutMillis]);
      });
    } else if ([call.method isEqualToString:@"stopSubscription"]) {
      result([bridge subscriptionStop:[self integer:arguments key:@"subscription"]]);
    } else {
      result(FlutterMethodNotImplemented);
    }
  } @catch (NSException *exception) {
    result([FlutterError errorWithCode:@"orisun_native_error"
                               message:exception.reason
                               details:exception.callStackSymbols]);
  }
}

- (NSString *)string:(NSDictionary<NSString *, id> *)arguments key:(NSString *)key {
  id value = arguments[key];
  if (![value isKindOfClass:[NSString class]]) {
    @throw [NSException exceptionWithName:NSInvalidArgumentException
                                   reason:[NSString stringWithFormat:@"%@ must be a string", key]
                                 userInfo:nil];
  }
  return value;
}

- (int64_t)integer:(NSDictionary<NSString *, id> *)arguments key:(NSString *)key {
  id value = arguments[key];
  if (![value isKindOfClass:[NSNumber class]]) {
    @throw [NSException exceptionWithName:NSInvalidArgumentException
                                   reason:[NSString stringWithFormat:@"%@ must be an integer", key]
                                 userInfo:nil];
  }
  return [value longLongValue];
}

- (BOOL)boolean:(NSDictionary<NSString *, id> *)arguments key:(NSString *)key {
  id value = arguments[key];
  if (![value isKindOfClass:[NSNumber class]]) {
    @throw [NSException exceptionWithName:NSInvalidArgumentException
                                   reason:[NSString stringWithFormat:@"%@ must be a boolean", key]
                                 userInfo:nil];
  }
  return [value boolValue];
}

- (void)dealloc {
  [self.bridge shutdown];
}

@end
