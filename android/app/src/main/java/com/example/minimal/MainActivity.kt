package com.example.minimal

import androidx.appcompat.app.AppCompatActivity
import android.os.Bundle
import android.widget.Button
import android.widget.EditText
import android.widget.TextView
import android.widget.Toast
import android.content.Intent
import android.content.ClipboardManager
import android.content.Context
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

class MainActivity : AppCompatActivity() {
    private lateinit var hostInput: EditText
    private lateinit var portInput: EditText
    private lateinit var userInput: EditText
    private lateinit var privateKeyInput: EditText
    private lateinit var startButton: Button
    private lateinit var stopButton: Button
    private lateinit var clearLogButton: Button
    private lateinit var copyLogButton: Button
    private lateinit var statusText: TextView
    private lateinit var logText: TextView
    private var isProxyRunning = false

    companion object {
        private const val PREFS_NAME = "ProxyPrefs"
        private const val KEY_HOST = "host"
        private const val KEY_PORT = "port"
        private const val KEY_USER = "user"
        private const val KEY_PRIVATE_KEY = "private_key"
        private const val KEY_PROXY_RUNNING = "proxy_running"
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

        hostInput = findViewById(R.id.hostInput)
        portInput = findViewById(R.id.portInput)
        userInput = findViewById(R.id.userInput)
        privateKeyInput = findViewById(R.id.privateKeyInput)
        startButton = findViewById(R.id.startButton)
        stopButton = findViewById(R.id.stopButton)
        clearLogButton = findViewById(R.id.clearLogButton)
        copyLogButton = findViewById(R.id.copyLogButton)
        statusText = findViewById(R.id.statusText)
        logText = findViewById(R.id.logText)

        val prefs = getSharedPreferences(PREFS_NAME, MODE_PRIVATE)
        hostInput.setText(prefs.getString(KEY_HOST, ""))
        portInput.setText(prefs.getString(KEY_PORT, "22"))
        userInput.setText(prefs.getString(KEY_USER, ""))
        privateKeyInput.setText(prefs.getString(KEY_PRIVATE_KEY, ""))
        isProxyRunning = prefs.getBoolean(KEY_PROXY_RUNNING, false)

        startButton.setOnClickListener {
            if (!isProxyRunning) {
                startProxy()
            }
        }

        stopButton.setOnClickListener {
            stopProxy()
        }

        clearLogButton.setOnClickListener {
            logText.text = ""
            appendToLog("Log cleared")
        }

        copyLogButton.setOnClickListener {
            val clipboard = getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
            clipboard.setText(logText.text)
            Toast.makeText(this, "Log copied to clipboard", Toast.LENGTH_SHORT).show()
        }

        updateButtonStates()
        appendToLog("Application started")

        if (isProxyRunning) {
            startProxy()
        }
    }

    private fun saveState() {
        val prefs = getSharedPreferences(PREFS_NAME, MODE_PRIVATE)
        prefs.edit().apply {
            putString(KEY_HOST, hostInput.text.toString())
            putString(KEY_PORT, portInput.text.toString())
            putString(KEY_USER, userInput.text.toString())
            putString(KEY_PRIVATE_KEY, privateKeyInput.text.toString())
            putBoolean(KEY_PROXY_RUNNING, isProxyRunning)
            apply()
        }
    }

    private fun appendToLog(message: String) {
        try {
            val timestamp = SimpleDateFormat("HH:mm:ss", Locale.getDefault()).format(Date())
            val logMessage = "[$timestamp] $message\n"
            runOnUiThread {
                logText.append(logMessage)
            }
        } catch (e: Exception) {
            e.printStackTrace()
        }
    }

    private fun startProxy() {
        val host = hostInput.text.toString()
        val port = portInput.text.toString()
        val user = userInput.text.toString()
        val privateKey = privateKeyInput.text.toString()

        if (host.isEmpty() || port.isEmpty() || user.isEmpty() || privateKey.isEmpty()) {
            Toast.makeText(this, "Please fill all fields", Toast.LENGTH_SHORT).show()
            return
        }

        val intent = Intent(this, ProxyService::class.java).apply {
            action = "START"
            putExtra("host", host)
            putExtra("port", port)
            putExtra("user", user)
            putExtra("privateKey", privateKey)
        }
        startService(intent)

        isProxyRunning = true
        saveState()
        updateButtonStates()
        appendToLog("Starting proxy service...")
    }

    private fun stopProxy() {
        val intent = Intent(this, ProxyService::class.java).apply {
            action = "STOP"
        }
        startService(intent)

        isProxyRunning = false
        saveState()
        updateButtonStates()
        appendToLog("Stopping proxy service...")
    }

    private fun updateButtonStates() {
        startButton.isEnabled = !isProxyRunning
        stopButton.isEnabled = isProxyRunning
        statusText.text = "Status: ${if (isProxyRunning) "Running" else "Stopped"}"
    }

    override fun onPause() {
        super.onPause()
        saveState()
    }
}
