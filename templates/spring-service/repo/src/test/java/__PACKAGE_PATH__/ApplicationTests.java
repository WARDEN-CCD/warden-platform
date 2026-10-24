package [[ .Package ]];

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;

import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.core.env.Environment;

@SpringBootTest(webEnvironment = SpringBootTest.WebEnvironment.RANDOM_PORT)
class ApplicationTests {

    private final HttpClient http = HttpClient.newHttpClient();

    @Autowired
    private Environment env;

    private HttpResponse<String> get(String path) throws Exception {
        int port = env.getRequiredProperty("local.server.port", Integer.class);
        HttpRequest request = HttpRequest.newBuilder(URI.create("http://localhost:" + port + path)).build();
        return http.send(request, HttpResponse.BodyHandlers.ofString());
    }

    @Test
    void infoEndpointNamesTheService() throws Exception {
        HttpResponse<String> response = get("/api/info");
        assertEquals(200, response.statusCode());
        assertTrue(response.body().contains("\"service\":\"[[ .Name ]]\""));
    }

    @Test
    void probesAndMetricsAreExposedForThePlatform() throws Exception {
        assertEquals(200, get("/actuator/health/readiness").statusCode());
        get("/api/items");
        HttpResponse<String> metrics = get("/actuator/prometheus");
        assertEquals(200, metrics.statusCode());
        assertTrue(metrics.body().contains("http_server_requests_seconds_bucket"),
                "canary analysis needs the request histogram");
    }
}
