package com.example.minimal

import androidx.appcompat.app.AppCompatActivity
import android.os.Bundle
import android.widget.Button
import android.widget.EditText
import android.widget.TextView
import android.widget.Toast
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.File
import java.io.FileOutputStream
import mobile.Mobile
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

class MainActivity : AppCompatActivity() {
    private lateinit var privateKeyInput: EditText
    private lateinit var startButton: Button
    private lateinit var stopButton: Button
    private lateinit var statusText: TextView
    private lateinit var logText: TextView
    private var isProxyRunning = false

    companion object {
        private const val PREFS_NAME = "ProxyPrefs"
        private const val KEY_PRIVATE_KEY = "private_key"
        private const val KEY_PROXY_RUNNING = "proxy_running"
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

        privateKeyInput = findViewById(R.id.privateKeyInput)
        startButton = findViewById(R.id.startButton)
        stopButton = findViewById(R.id.stopButton)
        statusText = findViewById(R.id.statusText)
        logText = findViewById(R.id.logText)

        // Восстанавливаем сохраненный ключ
        val prefs = getSharedPreferences(PREFS_NAME, MODE_PRIVATE)
        val savedKey = prefs.getString(KEY_PRIVATE_KEY, "")
        privateKeyInput.setText(savedKey)

        // Восстанавливаем состояние прокси
        isProxyRunning = prefs.getBoolean(KEY_PROXY_RUNNING, false)

        startButton.setOnClickListener {
            if (!isProxyRunning) {
                startProxy()
            }
        }

        stopButton.setOnClickListener {
            stopProxy()
        }

        // Stop кнопка всегда активна
        stopButton.isEnabled = true

        updateButtonStates()
        appendToLog("Application started")

        // Если прокси был запущен, пытаемся восстановить соединение
        if (isProxyRunning) {
            startProxy()
        }
    }

    private fun saveState() {
        val prefs = getSharedPreferences(PREFS_NAME, MODE_PRIVATE)
        prefs.edit().apply {
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
        val privateKey = privateKeyInput.text.toString()
        if (privateKey.isEmpty()) {
            Toast.makeText(this, "Please enter private key", Toast.LENGTH_SHORT).show()
            return
        }

        CoroutineScope(Dispatchers.IO).launch {
            try {
                val keyFile = File(filesDir, "private_key.pem")
                FileOutputStream(keyFile).use {
                    it.write(privateKey.toByteArray())
                }

                appendToLog("Starting proxy service...")

                val error = try {
                    Mobile.startProxy(
                        "35.193.63.104", // SSH Host
                        "22",            // SSH Port
                        "bg",           // SSH User
                        "",             // SSH Password (empty when using key)
                        keyFile.absolutePath,
                        "1081"          // Local Port
                    )
                    null
                } catch (e: Exception) {
                    e.printStackTrace()
                    e.message
                }

                withContext(Dispatchers.Main) {
                    if (error == null) {
                        isProxyRunning = true
                        saveState()
                        updateButtonStates()
                        appendToLog("Proxy started successfully")
                        Toast.makeText(this@MainActivity, "Proxy started", Toast.LENGTH_SHORT).show()
                    } else {
                        appendToLog("Error starting proxy: $error")
                        Toast.makeText(this@MainActivity, "Error: $error", Toast.LENGTH_LONG).show()
                    }
                }
            } catch (e: Exception) {
                e.printStackTrace()
                appendToLog("Exception: ${e.message}")
                withContext(Dispatchers.Main) {
                    Toast.makeText(this@MainActivity, "Error: ${e.message}", Toast.LENGTH_LONG).show()
                }
            }
        }
    }

    private fun stopProxy() {
        CoroutineScope(Dispatchers.IO).launch {
            try {
                appendToLog("Stopping proxy service...")

                val error = try {
                    Mobile.stopProxy()
                    null
                } catch (e: Exception) {
                    e.printStackTrace()
                    e.message
                }

                withContext(Dispatchers.Main) {
                    if (error == null) {
                        isProxyRunning = false
                        saveState()
                        updateButtonStates()
                        appendToLog("Proxy stopped successfully")
                        Toast.makeText(this@MainActivity, "Proxy stopped", Toast.LENGTH_SHORT).show()
                    } else {
                        appendToLog("Error stopping proxy: $error")
                        Toast.makeText(this@MainActivity, "Error: $error", Toast.LENGTH_LONG).show()
                    }
                }
            } catch (e: Exception) {
                e.printStackTrace()
                appendToLog("Exception while stopping proxy: ${e.message}")
                withContext(Dispatchers.Main) {
                    Toast.makeText(this@MainActivity, "Error stopping proxy: ${e.message}", Toast.LENGTH_LONG).show()
                }
            }
        }
    }

    private fun updateButtonStates() {
        startButton.isEnabled = !isProxyRunning
        // Stop кнопка всегда активна
        stopButton.isEnabled = true
        statusText.text = "Status: ${if (isProxyRunning) "Running" else "Stopped"}"
    }

    override fun onPause() {
        super.onPause()
        saveState()
    }
}
