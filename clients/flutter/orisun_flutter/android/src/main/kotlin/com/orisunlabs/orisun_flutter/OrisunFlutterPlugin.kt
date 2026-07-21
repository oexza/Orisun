package com.orisunlabs.orisun_flutter

import com.orisunlabs.orisun.fluttermobile.Bridge
import com.orisunlabs.orisun.fluttermobile.Fluttermobile
import io.flutter.embedding.engine.plugins.FlutterPlugin
import io.flutter.plugin.common.BinaryMessenger
import io.flutter.plugin.common.MethodCall
import io.flutter.plugin.common.MethodChannel
import io.flutter.plugin.common.StandardMethodCodec

class OrisunFlutterPlugin : FlutterPlugin, MethodChannel.MethodCallHandler {
    private lateinit var channel: MethodChannel
    private lateinit var bridge: Bridge

    override fun onAttachedToEngine(binding: FlutterPlugin.FlutterPluginBinding) {
        bridge = Fluttermobile.newBridge()
        val taskQueue = binding.binaryMessenger.makeBackgroundTaskQueue(
            BinaryMessenger.TaskQueueOptions().setIsSerial(false),
        )
        channel = MethodChannel(
            binding.binaryMessenger,
            "com.orisunlabs.orisun_flutter/methods",
            StandardMethodCodec.INSTANCE,
            taskQueue,
        )
        channel.setMethodCallHandler(this)
    }

    override fun onMethodCall(call: MethodCall, result: MethodChannel.Result) {
        try {
            val args = call.arguments<Map<String, Any?>>() ?: emptyMap()
            val response: Any = when (call.method) {
                "abiVersion" -> bridge.abiVersion()
                "open" -> bridge.open(args.string("directory"), args.string("boundaries"))
                "close" -> bridge.close(args.long("store"))
                "saveEvents" -> bridge.saveEvents(
                    args.long("store"),
                    args.string("boundary"),
                    args.string("events"),
                    args.string("expectedPosition"),
                    args.string("query"),
                )
                "getEvents" -> bridge.getEvents(
                    args.long("store"),
                    args.string("boundary"),
                    args.string("fromPosition"),
                    args.string("query"),
                    args.long("count"),
                    args.boolean("descending"),
                )
                "getLatestByCriteria" -> bridge.getLatestByCriteria(
                    args.long("store"),
                    args.string("boundary"),
                    args.string("query"),
                )
                "createBoundaryIndex" -> bridge.createBoundaryIndex(
                    args.long("store"),
                    args.string("boundary"),
                    args.string("name"),
                    args.string("fields"),
                    args.string("conditions"),
                    args.string("combinator"),
                )
                "dropBoundaryIndex" -> bridge.dropBoundaryIndex(
                    args.long("store"),
                    args.string("boundary"),
                    args.string("name"),
                )
                "subscribe" -> bridge.subscribe(
                    args.long("store"),
                    args.string("boundary"),
                    args.string("subscriberName"),
                    args.string("afterPosition"),
                    args.string("query"),
                )
                "subscriptionNext" -> bridge.subscriptionNext(
                    args.long("subscription"),
                    args.long("timeoutMillis"),
                )
                "stopSubscription" -> bridge.subscriptionStop(args.long("subscription"))
                else -> {
                    result.notImplemented()
                    return
                }
            }
            result.success(response)
        } catch (error: Throwable) {
            result.error("orisun_native_error", error.message, error.stackTraceToString())
        }
    }

    override fun onDetachedFromEngine(binding: FlutterPlugin.FlutterPluginBinding) {
        channel.setMethodCallHandler(null)
        bridge.shutdown()
    }
}

private fun Map<String, Any?>.string(key: String): String = this[key] as? String
    ?: throw IllegalArgumentException("$key must be a string")

private fun Map<String, Any?>.long(key: String): Long = (this[key] as? Number)?.toLong()
    ?: throw IllegalArgumentException("$key must be an integer")

private fun Map<String, Any?>.boolean(key: String): Boolean = this[key] as? Boolean
    ?: throw IllegalArgumentException("$key must be a boolean")
