// Licensed to Elasticsearch B.V. under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Elasticsearch B.V. licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

use serde::{Deserialize, Deserializer};

pub mod interpolator;
pub mod rmcp_ext;

/// Deserialize a string, and return `None` if it's empty. Useful for configuration fields like
/// `"foo": "${SOME_ENV_VAR:}"` that uses an env var if present without failing if missing.
pub fn none_if_empty_string<'de, D: Deserializer<'de>>(deserializer: D) -> Result<Option<String>, D::Error> {
    let s: Option<String> = Deserialize::deserialize(deserializer)?;
    match s {
        Some(s) if s.is_empty() => Ok(None),
        _ => Ok(s),
    }
}

/// Deserialize a boolean from a bool, number, or string. Useful for configuration fields like
/// `"foo": "${SOME_ENV_VAR:false}"` where the interpolated value is a quoted string, possibly
/// with stray whitespace. Accepts true/false, yes/no, 1/0 (case-insensitive, trimmed).
pub fn deserialize_bool_from_anything<'de, D: Deserializer<'de>>(deserializer: D) -> Result<bool, D::Error> {
    let value = serde_json::Value::deserialize(deserializer)?;
    match value {
        serde_json::Value::Bool(b) => Ok(b),
        serde_json::Value::Number(n) => Ok(n.as_f64().map(|f| f != 0.0).unwrap_or(false)),
        serde_json::Value::String(s) => match s.trim().to_lowercase().as_str() {
            "true" | "yes" | "1" => Ok(true),
            "false" | "no" | "0" | "" => Ok(false),
            other => Err(serde::de::Error::custom(format!(
                "Could not parse boolean from a string: {other}"
            ))),
        },
        _ => Err(serde::de::Error::custom("expected a boolean")),
    }
}

/// Deserialize an optional u64 from a number, string, or null. Useful for configuration fields
/// like `"foo": "${SOME_ENV_VAR:30}"` where the interpolated value is a quoted string, possibly
/// with stray whitespace.
pub fn deserialize_u64_from_anything<'de, D: Deserializer<'de>>(deserializer: D) -> Result<Option<u64>, D::Error> {
    let value = serde_json::Value::deserialize(deserializer)?;
    match value {
        serde_json::Value::Null => Ok(None),
        serde_json::Value::Number(n) => n
            .as_u64()
            .map(Some)
            .ok_or_else(|| serde::de::Error::custom("expected a non-negative integer")),
        serde_json::Value::String(s) => {
            let s = s.trim();
            if s.is_empty() {
                return Ok(None);
            }
            s.parse::<u64>()
                .map(Some)
                .map_err(|e| serde::de::Error::custom(format!("invalid number '{s}': {e}")))
        }
        _ => Err(serde::de::Error::custom("expected a number or string")),
    }
}
