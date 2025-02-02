package com.example.minimal

object Proxy {
    init {
        System.loadLibrary("gojni")  // Изменено с "proxy" на "gojni"
    }

    external fun startProxyWithKey(key: String): String?
    external fun stopProxyService()
}
