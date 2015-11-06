# golert
Replacement for Pingdom

This a simple alerting mechanism to reduce the number of false postives that
the on call engineer has to deal with.

It has a simple configuration (a JSON file), that looks like the following
example
```json
[
    {
        "url": "https://api.zeus.pm/docs",
        "status_code": 200,
        "method": "GET",
        "basic_auth": true,
        "parameters": [{}]
    },
    {
        "url": "https://api.zeus.pm/locations",
        "status_code": 200,
        "method": "GET",
        "basic_auth": false,
        "parameters": [
            {
                "key": "auth_method",
                "value": "user",
                "env_var": false
            },
            {
                "key": "auth_id",
                "value": "AUTH_ID",
                "env_var": true
            },
            {
                "key": "auth_secret",
                "value": "AUTH_SECRET",
                "env_var": true
            },
            {
                "key": "locations_scope",
                "value": "full",
                "env_var": false
            },
            {
                "key": "locale",
                "value": "en",
                "env_var": false
            }
        ]
    }
]
```

As you can see, it supports different types of requests to different URLs,
taking any number of arguments

You can also tell the alerting application to take the parameter from an env
var, in case you are providing sensitive information that you do no want
written in a file (like user credentials in the example above).

There is an option to tell the alerting application to use basic auth if you application is protected

Finally, you tell the alerting application what status code to expect back.

The alerting application performs two tests

1. is the status code correct
2. did the request take less than 10 seconds

if either of these conditions are false, the on call engineer will get a text message.

Another text message will be sent if the error clears itself.

The following env vars need to be provided when running the alerting application:

```bash
GOLERT_API_USER
GOLERT_API_SECRET
ONCALL_NUMBER
TWILIO_SID
TWILIO_TOKEN
TWILIO_NUMBER
```
