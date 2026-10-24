package [[ .Package ]];

import java.io.IOException;
import java.util.concurrent.ThreadLocalRandom;

import jakarta.servlet.FilterChain;
import jakarta.servlet.ServletException;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;

import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Component;
import org.springframework.web.filter.OncePerRequestFilter;

/**
 * Fault injection for the canary rollback experiment (hub EVALUATION.md E3). The values are compiled into the
 * image through application.yaml, so a faulty release is a real, signed release, not a runtime toggle.
 */
@Component
class DemoFaults extends OncePerRequestFilter {

    private final double errorRate;
    private final long latencyMs;

    DemoFaults(@Value("${warden.demo.error-rate:0.0}") double errorRate,
               @Value("${warden.demo.latency-ms:0}") long latencyMs) {
        this.errorRate = errorRate;
        this.latencyMs = latencyMs;
    }

    @Override
    protected boolean shouldNotFilter(HttpServletRequest request) {
        return !request.getRequestURI().startsWith("/api/");
    }

    @Override
    protected void doFilterInternal(HttpServletRequest request, HttpServletResponse response, FilterChain chain)
            throws ServletException, IOException {
        if (latencyMs > 0) {
            try {
                Thread.sleep(latencyMs);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
            }
        }
        if (errorRate > 0 && ThreadLocalRandom.current().nextDouble() < errorRate) {
            response.sendError(HttpServletResponse.SC_INTERNAL_SERVER_ERROR, "injected fault");
            return;
        }
        chain.doFilter(request, response);
    }
}
