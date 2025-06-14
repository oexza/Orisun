package com.orisunlabs.orisun.client;

import java.util.regex.Pattern;

public class Utils {
    /**
     * Extracts the expected and actual version numbers from an error message.
     *
     * @param errorMsg The error message containing version information
     * @return An array of integers where index 0 is the expected version and index 1 is the actual version
     * @throws IllegalArgumentException If the version numbers cannot be extracted
     */
    public static int[] extractVersionNumbers(String errorMsg) {
        // Define the regex pattern to match "Expected X, Actual Y"
        final var pattern = Pattern.compile("Expected\\s+(\\d+),\\s+Actual\\s+(\\d+)");

        // Create a matcher with the input string
        final var matcher = pattern.matcher(errorMsg);

        // Check if the pattern matches
        if (matcher.find()) {
            try {
                final var expected = Integer.parseInt(matcher.group(1));
                final var actual = Integer.parseInt(matcher.group(2));

                return new int[]{expected, actual};
            } catch (NumberFormatException e) {
                throw new IllegalArgumentException("Failed to parse version numbers", e);
            }
        }

        throw new IllegalArgumentException("Could not extract version numbers from error message: " + errorMsg);
    }
}
