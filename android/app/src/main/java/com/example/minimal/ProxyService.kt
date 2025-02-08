package com.example.minimal

import java.util.concurrent.TimeUnit
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Intent
import android.os.IBinder
import android.os.PowerManager
import androidx.core.app.NotificationCompat
import kotlinx.coroutines.*
import mobile.Mobile
import okhttp3.*
import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import kotlin.coroutines.CoroutineContext

class ProxyService : Service(), CoroutineScope {
    private lateinit var wakeLock: PowerManager.WakeLock
    private val job = SupervisorJob()
    override val coroutineContext: CoroutineContext
        get() = Dispatchers.IO + job

    private val NOTIFICATION_ID = 1
    private val CHANNEL_ID = "ProxyServiceChannel"
    private val WAKELOCK_TAG = "ProxyService::wakelock"
    private var isProxyRunning = false

    companion object {
        const val ACTION_LOG_UPDATE = "com.example.minimal.LOG_UPDATE"
        const val EXTRA_LOG_MESSAGE = "log_message"
    }

    private fun appendToLog(message: String) {
        // Send broadcast to MainActivity
        val intent = Intent(ACTION_LOG_UPDATE).apply {
            putExtra(EXTRA_LOG_MESSAGE, message)
        }
        sendBroadcast(intent)

        // Also update notification if needed
        updateNotification(message)
    }

    override fun onCreate() {
        super.onCreate()
        createNotificationChannel()
        initializeWakeLock()
    }

    private fun initializeWakeLock() {
        val powerManager = getSystemService(POWER_SERVICE) as PowerManager
        wakeLock = powerManager.newWakeLock(
            PowerManager.PARTIAL_WAKE_LOCK,
            WAKELOCK_TAG
        )
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            "START" -> {
                if (!isProxyRunning) {
                    val host = intent.getStringExtra("host") ?: return START_NOT_STICKY
                    val port = intent.getStringExtra("port") ?: return START_NOT_STICKY
                    val user = intent.getStringExtra("user") ?: return START_NOT_STICKY
                    val privateKey = intent.getStringExtra("privateKey") ?: return START_NOT_STICKY
                    val proxyType = intent.getStringExtra("proxyType") ?: "socks5"

                    startForeground(NOTIFICATION_ID, createNotification("Starting proxy..."))

                    if (!wakeLock.isHeld) {
                        wakeLock.acquire()
                    }

                    launch {
                        try {
                            val keyFile = File(filesDir, "private_key.pem")
                            FileOutputStream(keyFile).use {
                                it.write(privateKey.toByteArray())
                            }

                            try {
                                Mobile.stopProxy()
                            } catch (e: Exception) {
                            }

                            Mobile.startProxy(
                                host,
                                port,
                                user,
                                "",
                                keyFile.absolutePath,
                                "1081",
                                proxyType
                            )

                            isProxyRunning = true
                            updateNotification("$proxyType proxy is running")
                            startProxyMonitoring()
                            startLogMonitoring()
                        } catch (e: Exception) {
                            updateNotification("Proxy error: ${e.message}")
                            stopSelf()
                        }
                    }
                }
            }
            "STOP" -> {
                launch {
                    stopProxyService()
                }
            }
        }
        return START_STICKY
    }

    private fun startLogMonitoring() {
      launch {
        try {
          val client = OkHttpClient.Builder()
          .readTimeout(0, TimeUnit.SECONDS)
          .build()

          val request = Request.Builder()
          .url("http://localhost:1792/logs")  // Используем localhost вместо 127.0.0.1
          .build()

          client.newCall(request).enqueue(object : Callback {
            override fun onFailure(call: Call, e: IOException) {
              appendToLog("Log monitoring failed: ${e.message}")
            }

            override fun onResponse(call: Call, response: Response) {
              response.body?.source()?.let { source ->
                while (!source.exhausted()) {
                  val line = source.readUtf8Line() ?: continue
                  if (line.startsWith("data: ")) {
                    val logMessage = line.substring(6)
                    appendToLog(logMessage)
                  }
                }
              }
            }
          })
        } catch (e: Exception) {
          appendToLog("Failed to start log monitoring: ${e.message}")
        }
      }
    }

    private fun startProxyMonitoring() {
        launch {
            while (isProxyRunning && isActive) {
                try {
                    delay(5000)
                    val socket = java.net.Socket()
                    try {
                        socket.connect(java.net.InetSocketAddress("127.0.0.1", 1081), 1000)
                        updateNotification("Proxy is running")
                    } catch (e: Exception) {
                        updateNotification("Reconnecting proxy...")
                        throw e
                    } finally {
                        try {
                            socket.close()
                        } catch (e: Exception) {}
                    }
                } catch (e: Exception) {
                    stopProxyService()
                    break
                }
            }
        }
    }

    private suspend fun stopProxyService() {
        try {
            isProxyRunning = false
            Mobile.stopProxy()
            if (wakeLock.isHeld) {
                wakeLock.release()
            }
            stopForeground(true)
            stopSelf()
        } catch (e: Exception) {
            e.printStackTrace()
        }
    }

    override fun onBind(intent: Intent?): IBinder? = null

    private fun createNotificationChannel() {
        val channel = NotificationChannel(
            CHANNEL_ID,
            "Proxy Service Channel",
            NotificationManager.IMPORTANCE_LOW
        ).apply {
            description = "Shows proxy service status"
        }
        val notificationManager = getSystemService(NotificationManager::class.java)
        notificationManager.createNotificationChannel(channel)
    }

    private fun createNotification(status: String) = NotificationCompat.Builder(this, CHANNEL_ID)
        .setContentTitle("SSH2SOCKS5 Proxy")
        .setContentText(status)
        .setSmallIcon(android.R.drawable.ic_dialog_info)
        .setOngoing(true)
        .setContentIntent(
            PendingIntent.getActivity(
                this,
                0,
                Intent(this, MainActivity::class.java),
                PendingIntent.FLAG_IMMUTABLE
            )
        )
        .build()

    private fun updateNotification(status: String) {
        val notificationManager = getSystemService(NotificationManager::class.java)
        notificationManager.notify(NOTIFICATION_ID, createNotification(status))
    }

    override fun onDestroy() {
        super.onDestroy()
        job.cancel()
        launch {
            stopProxyService()
        }
    }
}
