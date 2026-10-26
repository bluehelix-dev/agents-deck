package dev.agentsdeck.agents_deck_android

import android.app.Activity
import android.content.Intent
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel

// The native side of `agents_deck/files`: where the pairing bundle lives
// (the app's private folder, which no other app can read) and the system
// file picker that reads one in. No plugins: a MethodChannel is enough.
class MainActivity : FlutterActivity() {
    private var pending: MethodChannel.Result? = null

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, CHANNEL).setMethodCallHandler { call, result ->
            when (call.method) {
                "dataDir" -> result.success(filesDir.absolutePath)
                "pickBundle" -> pickBundle(result)
                else -> result.notImplemented()
            }
        }
    }

    // Opens the document picker; the result arrives in onActivityResult.
    @Suppress("DEPRECATION")
    private fun pickBundle(result: MethodChannel.Result) {
        if (pending != null) {
            result.error("busy", "the file picker is already open", null)
            return
        }
        pending = result
        val intent = Intent(Intent.ACTION_OPEN_DOCUMENT).apply {
            addCategory(Intent.CATEGORY_OPENABLE)
            type = "*/*" // bundles are .json, but file apps label them in many ways
        }
        startActivityForResult(intent, PICK)
    }

    @Deprecated("Deprecated in Java")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode != PICK) return
        val result = pending ?: return
        pending = null
        val uri = data?.data
        if (resultCode != Activity.RESULT_OK || uri == null) {
            result.success(null) // cancelled
            return
        }
        try {
            val bytes = contentResolver.openInputStream(uri)?.use { it.readBytes() }
            when {
                bytes == null -> result.error("read", "could not open that file", null)
                bytes.size > MAX_BYTES -> result.error("read", "that file is too big to be a bundle", null)
                else -> result.success(String(bytes, Charsets.UTF_8))
            }
        } catch (e: Exception) {
            result.error("read", e.message ?: "could not read that file", null)
        }
    }

    companion object {
        private const val CHANNEL = "agents_deck/files"
        private const val PICK = 7341
        private const val MAX_BYTES = 1 shl 20 // a bundle is a few KB
    }
}
