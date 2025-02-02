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
import mobile.Mobile // Изменён импорт: биндинг сгенерирован из пакета mobile

class MainActivity : AppCompatActivity() {
    private lateinit var privateKeyInput: EditText
    private lateinit var startButton: Button
    private lateinit var stopButton: Button
    private lateinit var statusText: TextView
    private var isProxyRunning = false

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

        privateKeyInput = findViewById(R.id.privateKeyInput)
        startButton = findViewById(R.id.startButton)
        stopButton = findViewById(R.id.stopButton)
        statusText = findViewById(R.id.statusText)

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

                val error = Mobile.startProxyWithKey(keyFile.absolutePath)

                withContext(Dispatchers.Main) {
                    if (error != null) {
                        Toast.makeText(this@MainActivity, "Error: $error", Toast.LENGTH_LONG).show()
                    } else {
                        isProxyRunning = true
                        updateButtonStates()
                        Toast.makeText(this@MainActivity, "Proxy started", Toast.LENGTH_SHORT).show()
                    }
                }
            } catch (e: Exception) {
                withContext(Dispatchers.Main) {
                    Toast.makeText(this@MainActivity, "Error: ${e.message}", Toast.LENGTH_LONG).show()
                }
            }
        }
    }

    private fun stopProxy() {
        CoroutineScope(Dispatchers.IO).launch {
            val error = Mobile.stopProxyService()

            withContext(Dispatchers.Main) {
                if (error != null) {
                    Toast.makeText(this@MainActivity, "Error: $error", Toast.LENGTH_LONG).show()
                } else {
                    isProxyRunning = false
                    updateButtonStates()
                    Toast.makeText(this@MainActivity, "Proxy stopped", Toast.LENGTH_SHORT).show()
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
