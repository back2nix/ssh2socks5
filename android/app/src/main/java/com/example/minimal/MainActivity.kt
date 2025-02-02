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

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

        privateKeyInput = findViewById(R.id.privateKeyInput)
        startButton = findViewById(R.id.startButton)
        stopButton = findViewById(R.id.stopButton)
        statusText = findViewById(R.id.statusText)
        logText = findViewById(R.id.logText)

        startButton.setOnClickListener {
            if (!isProxyRunning) {
                startProxy()
            }
        }

        stopButton.setOnClickListener {
            if (isProxyRunning) {
                stopProxy()
            }
        }

        updateButtonStates()

        // Инициализация логов
        appendToLog("Application started")
    }

    private fun appendToLog(message: String) {
        val timestamp = SimpleDateFormat("HH:mm:ss", Locale.getDefault()).format(Date())
        val logMessage = "[$timestamp] $message\n"
        runOnUiThread {
            logText.append(logMessage)
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

                // Используем предопределенные значения для примера
                val result = Mobile.startProxy(
                    "35.193.63.104", // SSH Host
                    "22",            // SSH Port
                    "bg",           // SSH User
                    "",             // SSH Password (пустой, так как используем ключ)
                    keyFile.absolutePath,
                    "1081"          // Local Port
                )

                withContext(Dispatchers.Main) {
                    if (result == null) {
                        isProxyRunning = true
                        updateButtonStates()
                        appendToLog("Proxy started successfully")
                        Toast.makeText(this@MainActivity, "Proxy started", Toast.LENGTH_SHORT).show()
                    } else {
                        appendToLog("Error starting proxy: $result")
                        Toast.makeText(this@MainActivity, "Error: $result", Toast.LENGTH_LONG).show()
                    }
                }
            } catch (e: Exception) {
                appendToLog("Exception: ${e.message}")
                withContext(Dispatchers.Main) {
                    Toast.makeText(this@MainActivity, "Error: ${e.message}", Toast.LENGTH_LONG).show()
                }
            }
        }
    }

    private fun stopProxy() {
        CoroutineScope(Dispatchers.IO).launch {
            appendToLog("Stopping proxy service...")
            val result = Mobile.stopProxy()

            withContext(Dispatchers.Main) {
                if (result == null) {
                    isProxyRunning = false
                    updateButtonStates()
                    appendToLog("Proxy stopped successfully")
                    Toast.makeText(this@MainActivity, "Proxy stopped", Toast.LENGTH_SHORT).show()
                } else {
                    appendToLog("Error stopping proxy: $result")
                    Toast.makeText(this@MainActivity, "Error: $result", Toast.LENGTH_LONG).show()
                }
            }
        }
    }

    private fun updateButtonStates() {
        startButton.isEnabled = !isProxyRunning
        stopButton.isEnabled = isProxyRunning
        statusText.text = "Status: ${if (isProxyRunning) "Running" else "Stopped"}"
    }
}
