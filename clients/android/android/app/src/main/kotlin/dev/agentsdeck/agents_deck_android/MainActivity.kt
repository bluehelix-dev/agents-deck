package dev.agentsdeck.agents_deck_android

import android.app.Activity
import android.content.ActivityNotFoundException
import android.content.Intent
import android.os.Build
import android.provider.MediaStore
import android.view.WindowManager
import androidx.core.content.FileProvider
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel
import java.io.File

// The native side of `agents_deck/files`: where the pairing bundle lives
// (the app's private folder, which no other app can read), the system file
// picker that reads one in, the camera app that photographs a QR code, the
// pairing itself (Pairing.kt), and the screen's wake lock. No plugins: a
// MethodChannel is enough.
class MainActivity : FlutterActivity() {
    private var pending: MethodChannel.Result? = null
    private var photo: File? = null

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

    // Opens the camera app on a file of ours; the photo is read for a QR
    // code in onActivityResult and deleted.
    @Suppress("DEPRECATION")
    private fun scanQr(result: MethodChannel.Result) {
        if (pending != null) {
            result.error("busy", "the camera is already open", null)
            return
        }
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.Q) {
            result.error("pair", "pairing with a code needs Android 10 or newer: import a bundle instead", null)
            return
        }
        val file = File(File(cacheDir, "camera").apply { mkdirs() }, "pairing.jpg")
        val intent = Intent(MediaStore.ACTION_IMAGE_CAPTURE).apply {
            putExtra(MediaStore.EXTRA_OUTPUT, FileProvider.getUriForFile(this@MainActivity, "$packageName.files", file))
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_GRANT_WRITE_URI_PERMISSION)
        }
        try {
            startActivityForResult(intent, SCAN)
        } catch (e: ActivityNotFoundException) {
            result.error("camera", "no camera app on this phone: import a bundle instead", null)
            return
        }
        pending = result
        photo = file
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
                val file = photo ?: return
                photo = null
                if (resultCode != Activity.RESULT_OK || !file.exists()) {
                    file.delete()
                    result.success(null) // cancelled
                    return
                }
                Thread {
                    val text = try {
                        QrPhoto.decode(file)
                    } catch (e: Exception) {
                        null
                    } finally {
                        file.delete()
                    }
                    runOnUiThread {
                        if (text == null) {
                            result.error("qr", "no QR code in that photo: fill the frame with it and keep it in focus", null)
                        } else {
                            result.success(text)
                        }
                    }
                }.start()
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
