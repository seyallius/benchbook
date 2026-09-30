{
  "counts": {
    "improved": 2,
    "unchanged": 2
  },
  "from": "m001",
  "geomeans": [
    {
      "count": 4,
      "delta_pct": -25.433727489697254,
      "higher_is_better": false,
      "metric": "ns_op",
      "ratio": 0.7456627251030274
    },
    {
      "count": 2,
      "delta_pct": -41.815666484296074,
      "higher_is_better": false,
      "metric": "bytes_op",
      "ratio": 0.5818433351570392
    },
    {
      "count": 2,
      "delta_pct": -36.37909897196483,
      "higher_is_better": false,
      "metric": "allocs_op",
      "ratio": 0.6362090102803517
    }
  ],
  "results": [
    {
      "classification": "improved",
      "display_name": "Concat",
      "id": "example.com/demo/internal/core.BenchmarkConcat",
      "metrics": [
        {
          "classification": "improved",
          "delta_defined": true,
          "delta_pct": -43.33333333333332,
          "from_median": 119.75999999999999,
          "from_samples": 6,
          "from_summary": {
            "max": 122.4,
            "median": 119.75999999999999,
            "min": 117.12,
            "n": 6,
            "spread": 0.022044088176352714
          },
          "metric": "ns_op",
          "p": 0.0021645021645021645,
          "test_method": "exact",
          "tested": true,
          "to_median": 67.864,
          "to_samples": 6,
          "to_summary": {
            "max": 69.36,
            "median": 67.864,
            "min": 66.368,
            "n": 6,
            "spread": 0.022044088176352738
          }
        },
        {
          "classification": "improved",
          "delta_defined": true,
          "delta_pct": -66.66666666666666,
          "from_median": 96,
          "from_samples": 6,
          "from_summary": {
            "max": 96,
            "median": 96,
            "min": 96,
            "n": 6,
            "spread": 0
          },
          "metric": "bytes_op",
          "p": 0.0012619447673879762,
          "test_method": "normal",
          "tested": true,
          "to_median": 32,
          "to_samples": 6,
          "to_summary": {
            "max": 32,
            "median": 32,
            "min": 32,
            "n": 6,
            "spread": 0
          }
        },
        {
          "classification": "improved",
          "delta_defined": true,
          "delta_pct": -66.66666666666666,
          "from_median": 3,
          "from_samples": 6,
          "from_summary": {
            "max": 3,
            "median": 3,
            "min": 3,
            "n": 6,
            "spread": 0
          },
          "metric": "allocs_op",
          "p": 0.0012619447673879762,
          "test_method": "normal",
          "tested": true,
          "to_median": 1,
          "to_samples": 6,
          "to_summary": {
            "max": 1,
            "median": 1,
            "min": 1,
            "n": 6,
            "spread": 0
          }
        }
      ],
      "name": "BenchmarkConcat",
      "presence": {
        "in_from": true,
        "in_to": true
      },
      "variant": "default",
      "workload": "Concat"
    },
    {
      "classification": "improved",
      "display_name": "FindUser/result",
      "id": "example.com/demo/internal/core.BenchmarkFindUser/result",
      "metrics": [
        {
          "classification": "improved",
          "delta_defined": true,
          "delta_pct": -45.116279069767444,
          "from_median": 2145.7,
          "from_samples": 6,
          "from_summary": {
            "max": 2193,
            "median": 2145.7,
            "min": 2098.4,
            "n": 6,
            "spread": 0.022044088176352686
          },
          "metric": "ns_op",
          "p": 0.0021645021645021645,
          "test_method": "exact",
          "tested": true,
          "to_median": 1177.6399999999999,
          "to_samples": 6,
          "to_summary": {
            "max": 1203.6,
            "median": 1177.6399999999999,
            "min": 1151.68,
            "n": 6,
            "spread": 0.02204408817635264
          }
        },
        {
          "classification": "unchanged",
          "delta_defined": false,
          "delta_pct": 0,
          "from_median": 0,
          "from_samples": 6,
          "from_summary": {
            "max": 0,
            "median": 0,
            "min": 0,
            "n": 6,
            "spread": 0
          },
          "metric": "bytes_op",
          "p": 1,
          "test_method": "normal",
          "tested": true,
          "to_median": 0,
          "to_samples": 6,
          "to_summary": {
            "max": 0,
            "median": 0,
            "min": 0,
            "n": 6,
            "spread": 0
          }
        },
        {
          "classification": "unchanged",
          "delta_defined": false,
          "delta_pct": 0,
          "from_median": 0,
          "from_samples": 6,
          "from_summary": {
            "max": 0,
            "median": 0,
            "min": 0,
            "n": 6,
            "spread": 0
          },
          "metric": "allocs_op",
          "p": 1,
          "test_method": "normal",
          "tested": true,
          "to_median": 0,
          "to_samples": 6,
          "to_summary": {
            "max": 0,
            "median": 0,
            "min": 0,
            "n": 6,
            "spread": 0
          }
        }
      ],
      "name": "BenchmarkFindUser/result",
      "presence": {
        "in_from": true,
        "in_to": true
      },
      "variant": "result",
      "workload": "FindUser"
    },
    {
      "classification": "unchanged",
      "display_name": "FindUser/traditional",
      "id": "example.com/demo/internal/core.BenchmarkFindUser/traditional",
      "metrics": [
        {
          "classification": "unchanged",
          "delta_defined": true,
          "delta_pct": -0.8333333333333349,
          "from_median": 2395.2,
          "from_samples": 6,
          "from_summary": {
            "max": 2448,
            "median": 2395.2,
            "min": 2342.4,
            "n": 6,
            "spread": 0.02204408817635269
          },
          "metric": "ns_op",
          "p": 0.30952380952380953,
          "test_method": "exact",
          "tested": true,
          "to_median": 2375.24,
          "to_samples": 6,
          "to_summary": {
            "max": 2427.6,
            "median": 2375.24,
            "min": 2322.88,
            "n": 6,
            "spread": 0.022044088176352665
          }
        },
        {
          "classification": "unchanged",
          "delta_defined": false,
          "delta_pct": 0,
          "from_median": 0,
          "from_samples": 6,
          "from_summary": {
            "max": 0,
            "median": 0,
            "min": 0,
            "n": 6,
            "spread": 0
          },
          "metric": "bytes_op",
          "p": 1,
          "test_method": "normal",
          "tested": true,
          "to_median": 0,
          "to_samples": 6,
          "to_summary": {
            "max": 0,
            "median": 0,
            "min": 0,
            "n": 6,
            "spread": 0
          }
        },
        {
          "classification": "unchanged",
          "delta_defined": false,
          "delta_pct": 0,
          "from_median": 0,
          "from_samples": 6,
          "from_summary": {
            "max": 0,
            "median": 0,
            "min": 0,
            "n": 6,
            "spread": 0
          },
          "metric": "allocs_op",
          "p": 1,
          "test_method": "normal",
          "tested": true,
          "to_median": 0,
          "to_samples": 6,
          "to_summary": {
            "max": 0,
            "median": 0,
            "min": 0,
            "n": 6,
            "spread": 0
          }
        }
      ],
      "name": "BenchmarkFindUser/traditional",
      "presence": {
        "in_from": true,
        "in_to": true
      },
      "variant": "traditional",
      "workload": "FindUser"
    },
    {
      "classification": "unchanged",
      "display_name": "ParseJSON",
      "id": "example.com/demo/internal/core.BenchmarkParseJSON",
      "metrics": [
        {
          "classification": "unchanged",
          "delta_defined": true,
          "delta_pct": 0.23809523809522767,
          "from_median": 8383.2,
          "from_samples": 6,
          "from_summary": {
            "max": 8568,
            "median": 8383.2,
            "min": 8198.4,
            "n": 6,
            "spread": 0.022044088176352724
          },
          "metric": "ns_op",
          "p": 0.6991341991341992,
          "test_method": "exact",
          "tested": true,
          "to_median": 8403.16,
          "to_samples": 6,
          "to_summary": {
            "max": 8588.4,
            "median": 8403.16,
            "min": 8217.92,
            "n": 6,
            "spread": 0.02204408817635268
          }
        },
        {
          "classification": "regressed",
          "delta_defined": true,
          "delta_pct": 1.5625,
          "from_median": 4096,
          "from_samples": 6,
          "from_summary": {
            "max": 4096,
            "median": 4096,
            "min": 4096,
            "n": 6,
            "spread": 0
          },
          "metric": "bytes_op",
          "p": 0.0012619447673879762,
          "test_method": "normal",
          "tested": true,
          "to_median": 4160,
          "to_samples": 6,
          "to_summary": {
            "max": 4160,
            "median": 4160,
            "min": 4160,
            "n": 6,
            "spread": 0
          }
        },
        {
          "classification": "regressed",
          "delta_defined": true,
          "delta_pct": 21.428571428571427,
          "from_median": 42,
          "from_samples": 6,
          "from_summary": {
            "max": 42,
            "median": 42,
            "min": 42,
            "n": 6,
            "spread": 0
          },
          "metric": "allocs_op",
          "p": 0.0012619447673879762,
          "test_method": "normal",
          "tested": true,
          "to_median": 51,
          "to_samples": 6,
          "to_summary": {
            "max": 51,
            "median": 51,
            "min": 51,
            "n": 6,
            "spread": 0
          }
        }
      ],
      "name": "BenchmarkParseJSON",
      "presence": {
        "in_from": true,
        "in_to": true
      },
      "variant": "default",
      "workload": "ParseJSON"
    }
  ],
  "to": "m002"
}
