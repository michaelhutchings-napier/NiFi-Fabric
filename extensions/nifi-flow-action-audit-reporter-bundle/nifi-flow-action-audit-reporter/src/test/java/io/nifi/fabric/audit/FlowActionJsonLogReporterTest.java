package io.nifi.fabric.audit;

import org.apache.nifi.action.FlowAction;
import org.apache.nifi.action.FlowActionAttribute;
import org.junit.jupiter.api.Test;

import java.util.LinkedHashMap;
import java.util.Map;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

class FlowActionJsonLogReporterTest {
    private final FlowActionJsonLogReporter reporter = new FlowActionJsonLogReporter();

    @Test
    void buildEventMapsStableAuditSections() {
        final Map<String, String> attributes = new LinkedHashMap<>();
        attributes.put(FlowActionAttribute.ACTION_TIMESTAMP.key(), "2026-03-27T16:00:00Z");
        attributes.put(FlowActionAttribute.ACTION_ID.key(), "1234");
        attributes.put(FlowActionAttribute.ACTION_USER_IDENTITY.key(), "admin");
        attributes.put(FlowActionAttribute.ACTION_OPERATION.key(), "Add");
        attributes.put(FlowActionAttribute.ACTION_SOURCE_ID.key(), "component-1");
        attributes.put(FlowActionAttribute.ACTION_SOURCE_TYPE.key(), "ProcessGroup");
        attributes.put(FlowActionAttribute.ACTION_DETAILS_NAME.key(), "Ingest");
        attributes.put(FlowActionAttribute.ACTION_DETAILS_GROUP_ID.key(), "root-group");
        attributes.put(FlowActionAttribute.ACTION_DETAILS_PREVIOUS_GROUP_ID.key(), "previous-group");
        attributes.put(FlowActionAttribute.ACTION_DETAILS_SOURCE_ID.key(), "source-1");
        attributes.put(FlowActionAttribute.ACTION_DETAILS_SOURCE_TYPE.key(), "PROCESSOR");
        attributes.put(FlowActionAttribute.ACTION_DETAILS_DESTINATION_ID.key(), "destination-1");
        attributes.put(FlowActionAttribute.ACTION_DETAILS_DESTINATION_TYPE.key(), "PROCESS_GROUP");
        attributes.put(FlowActionAttribute.ACTION_DETAILS_RELATIONSHIP.key(), "success");
        attributes.put(FlowActionAttribute.ACTION_DETAILS_END_DATE.key(), "2026-03-27T16:05:00Z");
        attributes.put(FlowActionAttribute.COMPONENT_DETAILS_URI.key(), "/nifi-api/process-groups/component-1");
        attributes.put(FlowActionAttribute.COMPONENT_DETAILS_TYPE.key(), "org.apache.nifi.groups.ProcessGroup");
        attributes.put(FlowActionAttribute.REQUEST_DETAILS_REMOTE_ADDRESS.key(), "10.0.0.15");
        attributes.put(FlowActionAttribute.REQUEST_DETAILS_FORWARDED_FOR.key(), "203.0.113.9");
        attributes.put(FlowActionAttribute.REQUEST_DETAILS_USER_AGENT.key(), "curl/8.5.0");

        final Map<String, Object> event = reporter.buildEvent(flowAction(attributes));

        assertEquals("v1", event.get("schemaVersion"));
        assertEquals("nifi.flowAction", event.get("eventType"));
        assertEquals("2026-03-27T16:00:00Z", event.get("timestamp"));
        assertEquals("1234", event.get("actionId"));

        final Map<?, ?> user = nestedMap(event, "user");
        assertEquals("admin", user.get("identity"));

        final Map<?, ?> action = nestedMap(event, "action");
        assertEquals("Add", action.get("operation"));

        final Map<?, ?> component = nestedMap(event, "component");
        assertEquals("component-1", component.get("id"));
        assertEquals("ProcessGroup", component.get("type"));
        assertEquals("Ingest", component.get("name"));
        assertEquals("/nifi-api/process-groups/component-1", component.get("uri"));
        assertEquals("org.apache.nifi.groups.ProcessGroup", component.get("detailsType"));

        final Map<?, ?> processGroup = nestedMap(event, "processGroup");
        assertEquals("root-group", processGroup.get("id"));
        assertEquals("previous-group", processGroup.get("previousId"));

        final Map<?, ?> change = nestedMap(event, "change");
        assertEquals("success", change.get("relationship"));
        assertEquals("2026-03-27T16:05:00Z", change.get("endDate"));
        assertEquals("source-1", nestedMap(change, "source").get("id"));
        assertEquals("PROCESSOR", nestedMap(change, "source").get("type"));
        assertEquals("destination-1", nestedMap(change, "destination").get("id"));
        assertEquals("PROCESS_GROUP", nestedMap(change, "destination").get("type"));

        final Map<?, ?> request = nestedMap(event, "request");
        assertEquals("10.0.0.15", request.get("remoteAddress"));
        assertEquals("203.0.113.9", request.get("forwardedFor"));
        assertEquals("curl/8.5.0", request.get("userAgent"));

        @SuppressWarnings("unchecked")
        final Map<String, String> rawAttributes = (Map<String, String>) event.get("attributes");
        assertEquals(attributes.get(FlowActionAttribute.ACTION_DETAILS_NAME.key()), rawAttributes.get(FlowActionAttribute.ACTION_DETAILS_NAME.key()));
    }

    @Test
    void serializeEscapesStringsAndOmitsEmptySections() {
        final Map<String, String> attributes = Map.of(
                FlowActionAttribute.ACTION_OPERATION.key(), "Delete",
                FlowActionAttribute.ACTION_SOURCE_ID.key(), "component-2"
        );

        final Map<String, Object> event = reporter.buildEvent(flowAction(attributes));
        assertFalse(event.containsKey("processGroup"));
        assertFalse(event.containsKey("change"));
        assertFalse(event.containsKey("request"));

        final String json = FlowActionJsonLogReporter.serialize(Map.of("message", "line1\n\"line2\""));
        assertTrue(json.contains("\\n"));
        assertTrue(json.contains("\\\"line2\\\""));
    }

    private static FlowAction flowAction(final Map<String, String> attributes) {
        return () -> attributes;
    }

    @SuppressWarnings("unchecked")
    private static Map<String, Object> nestedMap(final Map<?, ?> parent, final String key) {
        return (Map<String, Object>) parent.get(key);
    }
}
