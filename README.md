# Ponse

Ponse is a man-in-the-middle proxy for the iRTSP protocol, made by Ubitus for GameNow/GameCloud. Currently, it has only been tested with iRTSP 1.21 (Dragon Quest X Online, 3DS).

## Features

- [X] Proxy iRTSP connection
- [X] Disable TLS on the client connection
- [X] Proxy media connections (video, audio...)
- [X] Proxy KNOCK connections (connection test)
- [ ] Play online (untested)
- [ ] Dump/View media content
- [ ] Modify iRTSP requests and responses

## Requirements

To use the proxy, you will need to set some environment variables:

| Environment variable | Description                                                                                                     |
|----------------------|-----------------------------------------------------------------------------------------------------------------|
| `PONSE_SERVER_URI`   | Determines the destination server that the client wants to connect to. Example: `irtsp://140.227.187.169:44802` |
| `PONSE_DISABLE_TLS`  | Optional. If the environment variable has a value set, TLS on the client will be disabled.                      |

If TLS isn't disabled, you will have to provide the X509 certificate (`server.crt`) and private key (`server.key`) to be used on the connection with the client.
