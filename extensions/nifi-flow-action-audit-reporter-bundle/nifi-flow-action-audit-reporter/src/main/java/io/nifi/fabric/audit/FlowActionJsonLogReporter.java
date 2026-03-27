/*
 * Copyright 2026 NiFi-Fabric
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
package io.nifi.fabric.audit;

import org.apache.nifi.action.FlowAction;
import org.apache.nifi.action.FlowActionAttribute;
import org.apache.nifi.action.FlowActionReporter;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.util.Collection;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.Objects;
import java.util.TreeMap;

/**
 * FlowActionJsonLogReporter writes one JSON event per flow action to a fixed
 * dedicated logger. The current FlowActionReporter API does not expose arbitrary
 * reporter properties, so this implementation intentionally keeps configuration
 * small and derives its event body from the standard FlowAction attributes.
 */
public class FlowActionJsonLogReporter implements FlowActionReporter {
    private static final Logger LOGGER = LoggerFactory.getLogger(FlowActionJsonLogReporter.class);

    private static final Logger AUDIT_LOGGER = LoggerFactory.getLogger("org.apache.nifi.flowaudit");

    private static final String SCHEMA_VERSION = "v1";

    private static final String EVENT_TYPE = "nifi.flowAction";

    @Override
    public void reportFlowActions(final Collection<FlowAction> actions) {
        Objects.requireNonNull(actions, "Flow actions are required");

        for (final FlowAction action : actions) {
            try {
                AUDIT_LOGGER.info(serialize(buildEvent(action)));
            } catch (final Exception e) {
                LOGGER.warn("Failed to serialize flow action for audit logging", e);
            }
        }
    }

    Map<String, Object> buildEvent(final FlowAction action) {
        final Map<String, String> attributes = new TreeMap<>(action.getAttributes());
        final Map<String, Object> event = new LinkedHashMap<>();
        event.put("schemaVersion", SCHEMA_VERSION);
        event.put("eventType", EVENT_TYPE);
        putIfPresent(event, "timestamp", attribute(attributes, FlowActionAttribute.ACTION_TIMESTAMP));
        putIfPresent(event, "actionId", attribute(attributes, FlowActionAttribute.ACTION_ID));

        final Map<String, Object> user = new LinkedHashMap<>();
        putIfPresent(user, "identity", attribute(attributes, FlowActionAttribute.ACTION_USER_IDENTITY));
        putIfNotEmpty(event, "user", user);

        final Map<String, Object> actionBody = new LinkedHashMap<>();
        putIfPresent(actionBody, "operation", attribute(attributes, FlowActionAttribute.ACTION_OPERATION));
        putIfNotEmpty(event, "action", actionBody);

        final Map<String, Object> component = new LinkedHashMap<>();
        putIfPresent(component, "id", attribute(attributes, FlowActionAttribute.ACTION_SOURCE_ID));
        putIfPresent(component, "type", attribute(attributes, FlowActionAttribute.ACTION_SOURCE_TYPE));
        putIfPresent(component, "name", attribute(attributes, FlowActionAttribute.ACTION_DETAILS_NAME));
        putIfPresent(component, "uri", attribute(attributes, FlowActionAttribute.COMPONENT_DETAILS_URI));
        putIfPresent(component, "detailsType", attribute(attributes, FlowActionAttribute.COMPONENT_DETAILS_TYPE));
        putIfNotEmpty(event, "component", component);

        final Map<String, Object> processGroup = new LinkedHashMap<>();
        putIfPresent(processGroup, "id", attribute(attributes, FlowActionAttribute.ACTION_DETAILS_GROUP_ID));
        putIfPresent(processGroup, "previousId", attribute(attributes, FlowActionAttribute.ACTION_DETAILS_PREVIOUS_GROUP_ID));
        putIfNotEmpty(event, "processGroup", processGroup);

        final Map<String, Object> change = new LinkedHashMap<>();
        final Map<String, Object> source = new LinkedHashMap<>();
        putIfPresent(source, "id", attribute(attributes, FlowActionAttribute.ACTION_DETAILS_SOURCE_ID));
        putIfPresent(source, "type", attribute(attributes, FlowActionAttribute.ACTION_DETAILS_SOURCE_TYPE));
        putIfNotEmpty(change, "source", source);

        final Map<String, Object> destination = new LinkedHashMap<>();
        putIfPresent(destination, "id", attribute(attributes, FlowActionAttribute.ACTION_DETAILS_DESTINATION_ID));
        putIfPresent(destination, "type", attribute(attributes, FlowActionAttribute.ACTION_DETAILS_DESTINATION_TYPE));
        putIfNotEmpty(change, "destination", destination);

        putIfPresent(change, "relationship", attribute(attributes, FlowActionAttribute.ACTION_DETAILS_RELATIONSHIP));
        putIfPresent(change, "endDate", attribute(attributes, FlowActionAttribute.ACTION_DETAILS_END_DATE));
        putIfNotEmpty(event, "change", change);

        final Map<String, Object> request = new LinkedHashMap<>();
        putIfPresent(request, "remoteAddress", attribute(attributes, FlowActionAttribute.REQUEST_DETAILS_REMOTE_ADDRESS));
        putIfPresent(request, "forwardedFor", attribute(attributes, FlowActionAttribute.REQUEST_DETAILS_FORWARDED_FOR));
        putIfPresent(request, "userAgent", attribute(attributes, FlowActionAttribute.REQUEST_DETAILS_USER_AGENT));
        putIfNotEmpty(event, "request", request);

        if (!attributes.isEmpty()) {
            event.put("attributes", attributes);
        }

        return event;
    }

    private static String attribute(final Map<String, String> attributes, final FlowActionAttribute attribute) {
        return attributes.get(attribute.key());
    }

    private static void putIfPresent(final Map<String, Object> target, final String key, final String value) {
        if (value != null && !value.isBlank()) {
            target.put(key, value);
        }
    }

    private static void putIfNotEmpty(final Map<String, Object> target, final String key, final Map<String, Object> value) {
        if (!value.isEmpty()) {
            target.put(key, value);
        }
    }

    static String serialize(final Object value) {
        if (value == null) {
            return "null";
        }
        if (value instanceof String string) {
            return quote(string);
        }
        if (value instanceof Number || value instanceof Boolean) {
            return value.toString();
        }
        if (value instanceof Map<?, ?> map) {
            final StringBuilder builder = new StringBuilder();
            builder.append('{');
            boolean first = true;
            for (final Map.Entry<?, ?> entry : map.entrySet()) {
                if (!first) {
                    builder.append(',');
                }
                first = false;
                builder.append(quote(String.valueOf(entry.getKey())));
                builder.append(':');
                builder.append(serialize(entry.getValue()));
            }
            builder.append('}');
            return builder.toString();
        }
        if (value instanceof Iterable<?> iterable) {
            final StringBuilder builder = new StringBuilder();
            builder.append('[');
            boolean first = true;
            for (final Object item : iterable) {
                if (!first) {
                    builder.append(',');
                }
                first = false;
                builder.append(serialize(item));
            }
            builder.append(']');
            return builder.toString();
        }
        return quote(String.valueOf(value));
    }

    private static String quote(final String value) {
        final StringBuilder builder = new StringBuilder(value.length() + 2);
        builder.append('"');
        for (int index = 0; index < value.length(); index++) {
            final char character = value.charAt(index);
            switch (character) {
                case '\\' -> builder.append("\\\\");
                case '"' -> builder.append("\\\"");
                case '\b' -> builder.append("\\b");
                case '\f' -> builder.append("\\f");
                case '\n' -> builder.append("\\n");
                case '\r' -> builder.append("\\r");
                case '\t' -> builder.append("\\t");
                default -> {
                    if (character < 0x20) {
                        builder.append(String.format("\\u%04x", (int) character));
                    } else {
                        builder.append(character);
                    }
                }
            }
        }
        builder.append('"');
        return builder.toString();
    }
}
