package [[ .Package ]];

import java.time.Instant;
import java.util.List;
import java.util.Map;

import org.springframework.beans.factory.ObjectProvider;
import org.springframework.boot.info.BuildProperties;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

/** Deliberately trivial: the platform is the product. */
@RestController
@RequestMapping("/api")
class ApiController {

    private final String version;

    ApiController(ObjectProvider<BuildProperties> build) {
        BuildProperties props = build.getIfAvailable();
        this.version = props != null ? props.getVersion() : "dev";
    }

    @GetMapping("/info")
    Map<String, Object> info() {
        return Map.of("service", "[[ .Name ]]", "version", version, "time", Instant.now().toString());
    }

    @GetMapping("/items")
    List<Map<String, Object>> items() {
        return List.of(Map.of("id", 1, "name", "first"), Map.of("id", 2, "name", "second"));
    }
}
