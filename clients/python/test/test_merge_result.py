"""
    lakeFS API

    lakeFS HTTP API  # noqa: E501

    The version of the OpenAPI document: 0.1.0
    Contact: services@treeverse.io
    Generated by: https://openapi-generator.tech
"""


import sys
import unittest

import lakefs_client
from lakefs_client.model.merge_result_summary import MergeResultSummary
globals()['MergeResultSummary'] = MergeResultSummary
from lakefs_client.model.merge_result import MergeResult


class TestMergeResult(unittest.TestCase):
    """MergeResult unit test stubs"""

    def setUp(self):
        pass

    def tearDown(self):
        pass

    def testMergeResult(self):
        """Test MergeResult"""
        # FIXME: construct object with mandatory attributes with example values
        # model = MergeResult()  # noqa: E501
        pass


if __name__ == '__main__':
    unittest.main()