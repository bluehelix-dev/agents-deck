package dev.agentsdeck.agents_deck_android

import org.json.JSONException
import org.json.JSONObject
import java.io.ByteArrayOutputStream
import java.net.ConnectException
import java.net.SocketTimeoutException
import java.net.URL
import java.net.UnknownHostException
import java.security.KeyPair
import java.security.KeyPairGenerator
import java.security.MessageDigest
import java.security.Signature
import java.security.cert.CertificateException
import java.security.cert.X509Certificate
import java.security.spec.ECGenParameterSpec
import java.util.Base64
import javax.net.ssl.HostnameVerifier
import javax.net.ssl.HttpsURLConnection
import javax.net.ssl.SSLContext
import javax.net.ssl.SSLException
import javax.net.ssl.X509TrustManager

// Pairing from a QR code (`agentctl pair <name> --qr`). The code carries the
// listener's URL, the deck's CA fingerprint and a one-time pairing code. This
// phone makes its own EC P-256 key, connects to the URL trusting only a
// certificate chain whose CA has exactly that fingerprint, and trades the code
// and a certificate request for its certificate (POST /v1/pair). The result is
// the same bundle `agentctl pair --bundle` would write, except that the key
// was born here and never travelled. Java's TLS stack, no libraries.
object Pairing {
    // What went wrong, in words for the pairing card.
    class Problem(message: String) : Exception(message)

    class Code(val url: String, val caFingerprint: ByteArray, val code: String)

    // Reads the QR code's payload: {"url", "ca", "code"} as JSON. The
    // fingerprint is "sha256:" and 64 hex digits, with or without colons.
    fun parse(payload: String): Code {
        val j = try {
            JSONObject(payload.trim())
        } catch (e: JSONException) {
            throw Problem("not a pairing code: expected {\"url\", \"ca\", \"code\"}")
        }
        val url = j.optString("url").trim().trimEnd('/')
        if (!url.startsWith("https://")) throw Problem("not a pairing code: url must be https://host:port")
        // "sha256:" and 64 hex digits, with or without colons between them (and
        // the prefix's colon may have gone with the rest).
        val ca = j.optString("ca").trim().replace(":", "").replace(" ", "").replaceFirst(Regex("^(?i)sha-?256"), "")
        if (!Regex("^[0-9a-fA-F]{64}$").matches(ca)) throw Problem("not a pairing code: ca must be the CA's SHA-256 fingerprint")
        val code = j.optString("code").trim()
        if (code.isEmpty()) throw Problem("not a pairing code: no code in it")
        return Code(url, ca.chunked(2).map { it.toInt(16).toByte() }.toByteArray(), code)
    }

    // Pairs with the deck the payload names. Returns the bundle (JSON) for
    // the app's private folder: name, url, ca, cert, key, fingerprint.
    fun pair(payload: String): String {
        val code = parse(payload)
        val keys = KeyPairGenerator.getInstance("EC").apply { initialize(ECGenParameterSpec("secp256r1")) }.generateKeyPair()
        val body = JSONObject().put("code", code.code).put("csr", pem("CERTIFICATE REQUEST", csr(keys))).toString()
        val trust = PinnedTrust(code.caFingerprint)
        val answer = post("${code.url}/v1/pair", body, trust)
        val leaf = trust.leaf ?: throw Problem("the deck sent no certificate")
        return JSONObject()
            .put("name", answer.optString("name"))
            .put("url", code.url)
            .put("ca", answer.optString("ca"))
            .put("cert", answer.optString("cert"))
            .put("key", pem("PRIVATE KEY", keys.private.encoded))
            // The listener's own certificate: the app accepts it when the
            // address isn't one the certificate covers (see agents_config.dart).
            .put("fingerprint", "sha256:" + sha256(leaf.encoded).joinToString(":") { "%02X".format(it) })
            .toString(2)
    }

    // Trusts a server whose chain contains the CA with the pinned fingerprint
    // and whose leaf that CA signed for serving. The CA is private to one
    // deck, so that is the deck's identity; the host name adds nothing.
    private class PinnedTrust(private val caFingerprint: ByteArray) : X509TrustManager {
        var leaf: X509Certificate? = null

        override fun checkServerTrusted(chain: Array<X509Certificate>, authType: String) {
            val ca = chain.firstOrNull { sha256(it.encoded).contentEquals(caFingerprint) }
                ?: throw CertificateException("the deck's certificate authority isn't the one in the code")
            val server = chain[0]
            if (server == ca) throw CertificateException("the server presented the CA itself")
            server.checkValidity()
            server.verify(ca.publicKey)
            if (server.extendedKeyUsage?.contains("1.3.6.1.5.5.7.3.1") != true) {
                throw CertificateException("the server's certificate isn't a server certificate")
            }
            leaf = server
        }

        override fun checkClientTrusted(chain: Array<X509Certificate>, authType: String) {
            throw CertificateException("not a server")
        }

        override fun getAcceptedIssuers(): Array<X509Certificate> = emptyArray()
    }

    private fun post(url: String, body: String, trust: PinnedTrust): JSONObject {
        val ctx = try {
            SSLContext.getInstance("TLSv1.3")
        } catch (e: Exception) {
            throw Problem("pairing with a code needs Android 10 or newer: import a bundle instead")
        }
        ctx.init(null, arrayOf(trust), null)
        val conn = try {
            URL(url).openConnection() as? HttpsURLConnection ?: throw Problem("the address must be https://")
        } catch (e: Exception) {
            throw Problem("bad address in the code: $url")
        }
        conn.sslSocketFactory = ctx.socketFactory
        conn.hostnameVerifier = HostnameVerifier { _, _ -> true } // the pinned CA is the identity
        conn.requestMethod = "POST"
        conn.doOutput = true
        conn.connectTimeout = 8000
        conn.readTimeout = 8000
        conn.setRequestProperty("Content-Type", "application/json")
        val text: String
        val status: Int
        try {
            conn.outputStream.use { it.write(body.toByteArray()) }
            status = conn.responseCode
            text = (if (status < 400) conn.inputStream else conn.errorStream)?.bufferedReader()?.use { it.readText() } ?: ""
        } catch (e: SSLException) {
            throw Problem("this isn't the deck the code is for: its certificate doesn't match (same Wi-Fi as the computer?)")
        } catch (e: ConnectException) {
            throw Problem("can't reach $url: is the phone on the computer's network?")
        } catch (e: UnknownHostException) {
            throw Problem("can't resolve the address in the code: agentctl pair --qr --host <the computer's IP>")
        } catch (e: SocketTimeoutException) {
            throw Problem("no answer from $url: is the phone on the computer's network?")
        } finally {
            conn.disconnect()
        }
        val j = try {
            JSONObject(text)
        } catch (e: JSONException) {
            throw Problem("the deck answered $status without JSON")
        }
        if (status != 200) throw Problem(j.optString("error", "the deck answered $status"))
        if (!j.optString("cert").contains("CERTIFICATE-----") || !j.optString("ca").contains("CERTIFICATE-----")) {
            throw Problem("the deck's answer holds no certificate")
        }
        return j
    }

    // ---- PKCS#10, by hand: a request has one subject, one key, no attributes ----

    // A certificate request for the key pair, DER. The deck names the device
    // from the pairing code, so the subject here is just a label.
    fun csr(keys: KeyPair): ByteArray {
        val version = byteArrayOf(0x02, 0x01, 0x00)
        val commonName = seq(byteArrayOf(0x06, 0x03, 0x55, 0x04, 0x03), der(0x0C, "agents deck phone".toByteArray()))
        val subject = seq(der(0x31, commonName))
        val attributes = byteArrayOf(0xA0.toByte(), 0x00)
        val info = seq(version, subject, keys.public.encoded, attributes)
        val signature = Signature.getInstance("SHA256withECDSA").apply { initSign(keys.private); update(info) }.sign()
        val ecdsaWithSha256 = seq(byteArrayOf(0x06, 0x08, 0x2A, 0x86.toByte(), 0x48, 0xCE.toByte(), 0x3D, 0x04, 0x03, 0x02))
        return seq(info, ecdsaWithSha256, der(0x03, byteArrayOf(0) + signature))
    }

    private fun der(tag: Int, body: ByteArray): ByteArray {
        val out = ByteArrayOutputStream()
        out.write(tag)
        val n = body.size
        when {
            n < 0x80 -> out.write(n)
            n < 0x100 -> { out.write(0x81); out.write(n) }
            else -> { out.write(0x82); out.write(n shr 8); out.write(n and 0xFF) }
        }
        out.write(body)
        return out.toByteArray()
    }

    private fun seq(vararg parts: ByteArray) = der(0x30, parts.fold(ByteArray(0)) { a, b -> a + b })

    fun pem(label: String, der: ByteArray): String =
        "-----BEGIN $label-----\n" + Base64.getMimeEncoder(64, "\n".toByteArray()).encodeToString(der) + "\n-----END $label-----\n"

    private fun sha256(b: ByteArray): ByteArray = MessageDigest.getInstance("SHA-256").digest(b)
}
