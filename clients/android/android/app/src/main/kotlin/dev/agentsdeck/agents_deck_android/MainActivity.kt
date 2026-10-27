package dev.agentsdeck.agents_deck_android

import android.app.Activity
import android.content.Intent
import android.os.Build
import android.view.WindowManager
import com.journeyapps.barcodescanner.ScanIntentResult
import com.journeyapps.barcodescanner.ScanOptions
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel

// The native side of `agents_deck/files`: where the pairing bundle lives
// (the app's private folder, which no other app can read), the system file
// picker that reads one in, the QR scanner (a live viewfinder in this app,
// ZXing's), the pairing itself (Pairing.kt), and the screen's wake lock. No
// plugins: a MethodChannel is enough.
class MainActivity : FlutterActivity() {
    private var pending: MethodChannel.Result? = null

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, CHANNEL).setMethodCallHandler { call, result ->
            when (call.method) {
                "dataDir" -> result.success(filesDir.absolutePath)
                "pickBundle" -> pickBundle(result)
                "scanQr" -> scanQr(result)
                "pair" -> pair(call.arguments as? String ?: "", result)
                "keepScreenOn" -> {
                    // A status board that goes dark isn't one; the user turns this off in settings.
                    val flag = WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON
                    if (call.arguments == true) window.addFlags(flag) else window.clearFlags(flag)
                    result.success(null)
                }
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

    // Opens the scanner: a viewfinder in this app that reads the QR code as
    // soon as it is in view and closes. It asks for the camera permission
    // itself the first time. The code's text arrives in onActivityResult.
    @Suppress("DEPRECATION")
    private fun scanQr(result: MethodChannel.Result) {
        if (pending != null) {
            result.error("busy", "the scanner is already open", null)
            return
        }
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.Q) {
            result.error("pair", "pairing with a code needs Android 10 or newer: import a bundle instead", null)
            return
        }
        val intent = ScanOptions().apply {
            setDesiredBarcodeFormats(ScanOptions.QR_CODE)
            setPrompt("") // the pairing screen said what to do
            setBeepEnabled(false)
            setOrientationLocked(false)
        }.createScanIntent(this)
        pending = result
        startActivityForResult(intent, SCAN)
    }

    // Pairs with the deck a QR code (or a pasted pairing code) names, off
    // the main thread; answers with the bundle to keep.
    private fun pair(payload: String, result: MethodChannel.Result) {
        Thread {
            val outcome = try {
                Result.success(Pairing.pair(payload))
            } catch (e: Pairing.Problem) {
                Result.failure(e)
            } catch (e: Exception) {
                Result.failure(Exception("pairing failed: ${e.message ?: e.javaClass.simpleName}"))
            }
            runOnUiThread {
                outcome.fold({ result.success(it) }, { result.error("pair", it.message, null) })
            }
        }.start()
    }

    @Deprecated("Deprecated in Java")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        val result = pending ?: return
        when (requestCode) {
            PICK -> {
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
            SCAN -> {
                pending = null
                // contents is null when the scanner was closed without a code
                // (the back gesture, or the camera permission refused).
                result.success(ScanIntentResult.parseActivityResult(resultCode, data).contents)
            }
        }
    }

    companion object {
        private const val CHANNEL = "agents_deck/files"
        private const val PICK = 7341
        private const val SCAN = 7342
        private const val MAX_BYTES = 1 shl 20 // a bundle is a few KB
    }
}
