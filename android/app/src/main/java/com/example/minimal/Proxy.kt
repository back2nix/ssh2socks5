package com.example.minimal

object Proxy {
    init {
        System.loadLibrary("proxy")
    }

    external fun startProxyWithKey(key: String): String?
    external fun stopProxyService()
}
