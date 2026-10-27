package dev.agentsdeck.agents_deck_android

import android.graphics.BitmapFactory
import com.google.zxing.BarcodeFormat
import com.google.zxing.BinaryBitmap
import com.google.zxing.DecodeHintType
import com.google.zxing.LuminanceSource
import com.google.zxing.NotFoundException
import com.google.zxing.RGBLuminanceSource
import com.google.zxing.common.GlobalHistogramBinarizer
import com.google.zxing.common.HybridBinarizer
import com.google.zxing.qrcode.QRCodeReader
import java.io.File

// Reads the QR code in a photo (the system camera app took it) with ZXing:
// a pure-Java decoder, so there is no camera code and no camera permission
// here. The terminal draws the code light-on-dark as often as dark-on-light,
// so both are tried; and the photo is tried turned by a quarter, because
// ZXing's finder-pattern search scans rows and one mask pattern in eight
// (vertical stripes) sometimes fools it until the stripes lie the other way.
object QrPhoto {
    fun decode(file: File): String? {
        val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        BitmapFactory.decodeFile(file.path, bounds)
        if (bounds.outWidth <= 0) return null
        // A photo is 12 megapixels or more; a QR code reads fine at a fifth
        // of that, and faster.
        var sample = 1
        while (bounds.outWidth / (sample * 2) >= 1200) sample *= 2
        val bmp = BitmapFactory.decodeFile(file.path, BitmapFactory.Options().apply { inSampleSize = sample }) ?: return null
        val w = bmp.width
        val h = bmp.height
        val pixels = IntArray(w * h)
        bmp.getPixels(pixels, 0, w, 0, 0, w, h)
        bmp.recycle()
        val source = RGBLuminanceSource(w, h, pixels)
        decode(source)?.let { return it }
        decode(source.invert())?.let { return it }
        val turned = IntArray(w * h)
        for (y in 0 until h) {
            for (x in 0 until w) {
                turned[x * h + (h - 1 - y)] = pixels[y * w + x]
            }
        }
        val quarter = RGBLuminanceSource(h, w, turned)
        return decode(quarter) ?: decode(quarter.invert())
    }

    private fun decode(source: LuminanceSource): String? {
        val hints = mapOf(
            DecodeHintType.TRY_HARDER to true,
            DecodeHintType.POSSIBLE_FORMATS to listOf(BarcodeFormat.QR_CODE),
        )
        for (binarizer in listOf(HybridBinarizer(source), GlobalHistogramBinarizer(source))) {
            try {
                return QRCodeReader().decode(BinaryBitmap(binarizer), hints).text
            } catch (e: NotFoundException) {
                // the next binarizer, the other polarity, or nothing
            } catch (e: Exception) {
                // ChecksumException, FormatException: a code, but unreadable
            }
        }
        return null
    }
}
